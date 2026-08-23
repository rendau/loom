package loom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc"

	pb "github.com/rendau/loom/api/artifact_v1"
)

const (
	logBatchMax      = 256                    // строк в одном батче
	logFlushInterval = 500 * time.Millisecond // страховочный тик отправщика
	logBufferCap     = 32 * 1024              // неподтверждённых строк; полный буфер блокирует push
	logCloseTimeout  = 30 * time.Second       // на дослать буфер при close (с реконнектами)
)

// grpcLogSink шлёт логи таска на artifact-сервер bidi-стримом
// TaskLogService с подтверждениями — доставка без потерь и дублей:
//
//   - строки нумеруются сквозным seq и живут в буфере, пока сервер не
//     подтвердит запись ack'ом;
//   - при обрыве стрима (рестарт artifact-сервера) отправщик
//     переподключается с backoff'ом; сервер отвечает на header числом уже
//     записанных строк, клиент отбрасывает подтверждённый префикс буфера и
//     досылает остальное — сервер дедуплицирует по seq;
//   - полный буфер блокирует push (backpressure): пока попытка жива, строки
//     не отбрасываются, таск ждёт восстановления канала.
//
// Каждая строка синхронно дублируется в dup — настоящий stdout контейнера:
// страховка на случай смерти процесса вместе с SDK, обычный путь чтения
// логов — стрим.
type grpcLogSink struct {
	conn   *grpc.ClientConn
	client pb.TaskLogServiceClient
	ctx    context.Context
	cancel context.CancelFunc
	dup    io.Writer
	header *pb.TaskLogHeader

	mu      sync.Mutex
	cond    *sync.Cond // будит блокированный push и ожидание доставки
	buf     []logEntry // неподтверждённый хвост; buf[0] имеет номер base
	base    int64      // seq первой строки буфера: всё до неё подтверждено
	next    int64      // seq следующей новой строки (base + len(buf))
	closing bool       // close() вызван: дослать и завершиться
	dead    bool       // терминальный отказ приёмника: доставка прекращена
	failure error      // последняя ошибка доставки (для отчёта close)

	wake chan struct{} // сигнал отправщику: появились строки / closing
	done chan struct{}
}

// newGrpcLogSink создаёт sink и запускает отправщик. dup — настоящий stdout
// (до перехвата fd), туда синхронно дублируется каждая строка.
func newGrpcLogSink(addr string, dup io.Writer, runID, task string, attempt int) (*grpcLogSink, error) {
	conn, err := grpc.NewClient(addr, dialOpts()...)
	if err != nil {
		return nil, fmt.Errorf("dial artifact server %q: %w", addr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &grpcLogSink{
		conn:   conn,
		client: pb.NewTaskLogServiceClient(conn),
		ctx:    ctx,
		cancel: cancel,
		dup:    dup,
		header: &pb.TaskLogHeader{RunId: runID, Task: task, Attempt: int32(attempt)},
		wake:   make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	s.cond = sync.NewCond(&s.mu)

	go s.run()

	return s, nil
}

func (s *grpcLogSink) push(e logEntry) {
	if s.dup != nil {
		fmt.Fprintln(s.dup, e.line)
	}

	s.mu.Lock()
	// backpressure: доставка без потерь важнее скорости — полный буфер
	// ждёт ack'ов сервера (или реконнекта, который их принесёт)
	for int64(len(s.buf)) >= logBufferCap && !s.dead && !s.closing {
		s.cond.Wait()
	}
	if s.dead {
		s.mu.Unlock()
		return // приёмник отказал навсегда — строка остаётся в dup
	}
	s.buf = append(s.buf, e)
	s.next++
	s.mu.Unlock()

	s.notify()
}

func (s *grpcLogSink) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// close дожидается доставки буфера (с таймаутом, продолжая реконнекты) и
// закрывает соединение. Ошибка — недоставленные строки или терминальный
// отказ приёмника.
func (s *grpcLogSink) close() error {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	s.cond.Broadcast()
	s.notify()

	select {
	case <-s.done:
	case <-time.After(logCloseTimeout):
		s.cancel() // не смогли дослать — рвём стрим
		<-s.done
	}

	s.cancel()
	_ = s.conn.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.buf) > 0 {
		err := s.failure
		if err == nil {
			err = errors.New("delivery timed out")
		}
		return fmt.Errorf("undelivered %d log line(s): %w", len(s.buf), err)
	}
	if s.dead {
		return s.failure
	}
	return nil
}

// run — цикл отправщика: жизненные циклы стрима с реконнектом между ними.
func (s *grpcLogSink) run() {
	defer close(s.done)

	delay := reconnectMinDelay
	for {
		if s.delivered() {
			return
		}

		err := s.pump()
		switch {
		case err == nil:
			return // close: буфер доставлен, стрим закрыт
		case s.ctx.Err() != nil:
			s.noteFailure(err)
			return // close() оборвал доставку по таймауту
		case terminalStreamErr(err):
			s.fail(err)
			return
		}

		// транзиентный обрыв: реконнект с backoff
		s.noteFailure(err)
		select {
		case <-time.After(delay):
		case <-s.ctx.Done():
		}
		delay = min(delay*2, reconnectMaxDelay)
	}
}

// pump — один жизненный цикл стрима: handshake (header → ack с позицией
// сервера), затем отправка батчей и приём ack'ов до обрыва или полной
// доставки при close. nil — доставка завершена (closing и все строки
// подтверждены).
func (s *grpcLogSink) pump() error {
	stream, err := s.client.PushTaskLog(s.ctx)
	if err != nil {
		return err
	}

	if err = stream.Send(&pb.PushTaskLogRequest{Msg: &pb.PushTaskLogRequest_Header{Header: s.header}}); err != nil {
		// настоящий статус — из Recv
		if _, recvErr := stream.Recv(); recvErr != nil {
			err = recvErr
		}
		return err
	}
	ack, err := stream.Recv()
	if err != nil {
		return err
	}
	s.applyAck(ack.GetNextSeq())
	s.clearFailure()

	// приёмник ack'ов: двигает подтверждённую границу буфера
	recvErrCh := make(chan error, 1)
	go func() {
		for {
			ack, err := stream.Recv()
			if err != nil {
				recvErrCh <- err
				return
			}
			s.applyAck(ack.GetNextSeq())
		}
	}()

	sent := s.ackedSeq() // всё до sent уже у сервера (в этом стриме)
	ticker := time.NewTicker(logFlushInterval)
	defer ticker.Stop()

	for {
		for {
			entries, first := s.pending(sent)
			if len(entries) == 0 {
				break
			}
			if err = stream.Send(&pb.PushTaskLogRequest{Msg: &pb.PushTaskLogRequest_Batch{
				Batch: &pb.TaskLogBatch{FirstSeq: first, Entries: entries},
			}}); err != nil {
				return <-recvErrCh // настоящий статус — из Recv
			}
			sent = first + int64(len(entries))
		}

		if s.delivered() {
			_ = stream.CloseSend()
			return nil
		}

		select {
		case <-ticker.C:
		case <-s.wake:
		case err = <-recvErrCh:
			return err
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
}

// pending возвращает очередной батч неподтверждённых строк начиная с sent
// (но не раньше подтверждённой границы) и его first_seq.
func (s *grpcLogSink) pending(sent int64) ([]*pb.TaskLogEntry, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	first := max(sent, s.base)
	if first >= s.next {
		return nil, first
	}

	n := min(s.next-first, logBatchMax)
	entries := make([]*pb.TaskLogEntry, n)
	for i := range entries {
		entries[i] = encodeLogEntry(s.buf[first-s.base+int64(i)])
	}

	return entries, first
}

// applyAck отбрасывает подтверждённый префикс буфера и будит блокированный
// push.
func (s *grpcLogSink) applyAck(nextSeq int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	drop := min(nextSeq-s.base, int64(len(s.buf)))
	if drop <= 0 {
		return
	}
	s.buf = s.buf[drop:]
	s.base += drop
	s.cond.Broadcast()
}

func (s *grpcLogSink) ackedSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base
}

// delivered — close() вызван и подтверждена доставка всего буфера.
func (s *grpcLogSink) delivered() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing && s.base == s.next
}

// fail — терминальный отказ приёмника: доставка прекращается, буфер
// остаётся для отчёта close(), блокированные push отпускаются.
func (s *grpcLogSink) fail(err error) {
	s.mu.Lock()
	s.dead = true
	s.failure = err
	s.mu.Unlock()
	s.cond.Broadcast()
}

func (s *grpcLogSink) noteFailure(err error) {
	s.mu.Lock()
	s.failure = err
	s.mu.Unlock()
}

// clearFailure сбрасывает транзиентную ошибку после успешного handshake.
func (s *grpcLogSink) clearFailure() {
	s.mu.Lock()
	if !s.dead {
		s.failure = nil
	}
	s.mu.Unlock()
}

func encodeLogEntry(e logEntry) *pb.TaskLogEntry {
	return &pb.TaskLogEntry{TsUnixMs: e.time.UnixMilli(), Source: encodeLogSource(e.source), Line: e.line}
}

func encodeLogSource(v logSource) pb.TaskLogSource {
	switch v {
	case logSourceLog:
		return pb.TaskLogSource_TASK_LOG_SOURCE_LOG
	case logSourceStdout:
		return pb.TaskLogSource_TASK_LOG_SOURCE_STDOUT
	case logSourceStderr:
		return pb.TaskLogSource_TASK_LOG_SOURCE_STDERR
	default:
		return pb.TaskLogSource_TASK_LOG_SOURCE_UNSPECIFIED
	}
}
