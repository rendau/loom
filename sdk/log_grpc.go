package loom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/rendau/loom/api/server_v1"
)

const (
	logBatchMax      = 256                    // строк в батче
	logFlushInterval = 500 * time.Millisecond // флаш неполного батча
	logChanCap       = 4096                   // буфер между push и отправщиком
	logCloseTimeout  = 5 * time.Second        // на дослать буферы при close
)

// grpcLogSink шлёт логи таска батчами в TaskLogService control plane'а
// одним стримом на весь attempt. Каждая строка синхронно дублируется в dup —
// настоящий stdout контейнера: если SDK умрёт вместе с процессом, логи
// останутся хотя бы у kubernetes, а причину смерти допишет control plane.
//
// Смерть лог-стрима не валит таск: отправка прекращается, дублирование в
// dup продолжается, ошибка возвращается из close.
type grpcLogSink struct {
	conn   *grpc.ClientConn
	stream grpc.ClientStreamingClient[pb.PushTaskLogRequest, pb.PushTaskLogResponse]
	cancel context.CancelFunc
	dup    io.Writer

	ch   chan logEntry
	quit chan struct{}
	done chan struct{}
	err  error // владеет отправщик; читать после <-done
}

// newGrpcLogSink открывает лог-стрим и отправщик. dup — настоящий stdout
// (до перехвата fd), туда синхронно дублируется каждая строка.
func newGrpcLogSink(addr string, dup io.Writer, runID, task string, attempt int) (*grpcLogSink, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial control plane %q: %w", addr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	stream, err := pb.NewTaskLogServiceClient(conn).PushTaskLog(ctx)
	if err == nil {
		err = stream.Send(&pb.PushTaskLogRequest{Msg: &pb.PushTaskLogRequest_Header{
			Header: &pb.TaskLogHeader{RunId: runID, Task: task, Attempt: int32(attempt)},
		}})
	}
	if err != nil {
		cancel()
		_ = conn.Close()
		return nil, fmt.Errorf("open task log stream: %w", err)
	}

	s := &grpcLogSink{
		conn:   conn,
		stream: stream,
		cancel: cancel,
		dup:    dup,
		ch:     make(chan logEntry, logChanCap),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go s.run()

	return s, nil
}

func (s *grpcLogSink) push(e logEntry) {
	if s.dup != nil {
		fmt.Fprintln(s.dup, e.line)
	}

	select {
	case s.ch <- e:
	case <-s.quit: // sink закрывается — строка остаётся только в dup
	}
}

// close дожидается доставки буферов (с таймаутом) и закрывает стрим.
func (s *grpcLogSink) close() error {
	close(s.quit)

	select {
	case <-s.done:
	case <-time.After(logCloseTimeout):
		s.cancel() // не смогли дослать — рвём стрим
		<-s.done
	}

	s.cancel()
	_ = s.conn.Close()

	return s.err
}

// run — отправщик: копит батчи и шлёт их по размеру и по таймеру.
func (s *grpcLogSink) run() {
	defer close(s.done)

	ticker := time.NewTicker(logFlushInterval)
	defer ticker.Stop()

	batch := make([]*pb.TaskLogEntry, 0, logBatchMax)

	flush := func() {
		if len(batch) == 0 || s.err != nil {
			batch = batch[:0]
			return
		}
		if err := s.stream.Send(&pb.PushTaskLogRequest{Msg: &pb.PushTaskLogRequest_Batch{
			Batch: &pb.TaskLogBatch{Entries: batch},
		}}); err != nil {
			// настоящий статус — в CloseAndRecv; стрим мёртв, дальше только dup
			if _, recvErr := s.stream.CloseAndRecv(); recvErr != nil {
				err = recvErr
			}
			s.err = fmt.Errorf("push task log: %w", err)
		}
		batch = batch[:0]
	}

	add := func(e logEntry) {
		batch = append(batch, encodeLogEntry(e))
		if len(batch) >= logBatchMax {
			flush()
		}
	}

	for {
		select {
		case e := <-s.ch:
			add(e)
		case <-ticker.C:
			flush()
		case <-s.quit:
			for {
				select {
				case e := <-s.ch:
					add(e)
				default:
					flush()
					if s.err == nil {
						if _, err := s.stream.CloseAndRecv(); err != nil && !errors.Is(err, io.EOF) {
							s.err = fmt.Errorf("close task log stream: %w", err)
						}
					}
					return
				}
			}
		}
	}
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
