package loom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/rendau/loom/api/artifact_v1"
	"github.com/rendau/loom/sdk/streamstore"
)

const (
	// writeChunkSize — максимальный размер chunk-сообщения записи.
	writeChunkSize = 256 * 1024
	// writeBufferCap — байт неподтверждённых данных писателя; полный буфер
	// блокирует Write (backpressure) до ack'ов сервера или реконнекта.
	writeBufferCap = 4 << 20
	// abortWaitTimeout — сколько Abort ждёт подтверждения сервера; abort —
	// best-effort очистка, судьбу стрима в любом случае решит FinishAttempt.
	abortWaitTimeout = 10 * time.Second
)

// grpcStore — remote-реализация artifactStore: gRPC-клиент artifact-сервера.
// Стейт-машина стримов живёт на сервере (общий sdk/streamstore), поэтому
// семантика обмена совпадает с локальным режимом вплоть до кода. Писатель и
// читатель переживают обрыв соединения (рестарт artifact-сервера):
// писатель досылает неподтверждённый хвост через resume, читатель
// переоткрывает стрим со своего offset'а.
type grpcStore struct {
	conn   *grpc.ClientConn
	client pb.ArtifactServiceClient
}

func dialGrpcStore(addr string) (*grpcStore, error) {
	conn, err := grpc.NewClient(addr, dialOpts()...)
	if err != nil {
		return nil, fmt.Errorf("dial artifact server %q: %w", addr, err)
	}

	return &grpcStore{conn: conn, client: pb.NewArtifactServiceClient(conn)}, nil
}

func (s *grpcStore) Close() error {
	return s.conn.Close()
}

// OpenWrite открывает запись артефакта и дожидается создания стрима на
// сервере: терминальные ошибки открытия (дубль имени, завершённая попытка)
// возвращаются сразу, недоступность сервера пережидается реконнектами.
func (s *grpcStore) OpenWrite(ctx context.Context, ref ArtifactRef) (ArtifactWriter, error) {
	wctx, cancel := context.WithCancel(ctx)
	w := &grpcWriter{
		client: s.client,
		ref:    ref,
		ctx:    wctx,
		cancel: cancel,
		wake:   make(chan struct{}, 1),
	}
	w.cond = sync.NewCond(&w.mu)

	go w.run()

	w.mu.Lock()
	defer w.mu.Unlock()
	for !w.began && w.err == nil {
		w.cond.Wait()
	}
	if w.err != nil {
		cancel()
		return nil, w.err
	}

	return w, nil
}

// OpenRead открывает follow-чтение с offset 0. Стрим открывается лениво:
// ошибки открытия (NOT_FOUND, ABORTED) приходят из первого Read.
func (s *grpcStore) OpenRead(ctx context.Context, ref ArtifactRef) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(ctx)

	return &grpcReader{client: s.client, ref: ref, ctx: ctx, cancel: cancel}, nil
}

// finishAttempt помечает попытку завершённой на artifact-сервере. Вызов
// идемпотентен: control plane повторит его как страховку при смерти пода.
func (s *grpcStore) finishAttempt(ctx context.Context, runID, task string, attempt int) error {
	_, err := s.client.FinishAttempt(ctx, &pb.FinishAttemptRequest{RunId: runID, Task: task, Attempt: int32(attempt)})
	return decodeGrpcErr(err)
}

func encodeGrpcRef(v ArtifactRef) *pb.ArtifactRef {
	return &pb.ArtifactRef{RunId: v.RunID, Task: v.Task, Attempt: int32(v.Attempt), Name: v.Name}
}

// ── Запись ──────────────────────────────────────────────

type writerFinish int

const (
	finishNone writerFinish = iota
	finishCommit
	finishAbort
)

// errResumeRetry — handshake надо повторить в resume-режиме: BeginWrite мог
// пройти на сервере, а подтверждение — потеряться в обрыве.
var errResumeRetry = errors.New("retry with resume")

// grpcWriter пишет артефакт bidi-стримом с подтверждениями — запись
// переживает рестарт artifact-сервера:
//
//   - данные живут в буфере, пока сервер не подтвердит их ack'ом (size);
//   - при обрыве стрима отправщик переподключается (resume): сервер
//     сообщает точку продолжения, клиент досылает неподтверждённый хвост;
//   - полный буфер блокирует Write (backpressure) — данные не теряются,
//     пока жива попытка;
//   - Commit блокирует до подтверждения сервера: успешный return таска
//     гарантирует, что данные легли на диск artifact-сервера.
type grpcWriter struct {
	client pb.ArtifactServiceClient
	ref    ArtifactRef
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte // неподтверждённый хвост данных; начинается с позиции base
	base   int64  // байт подтверждено сервером
	next   int64  // байт записано клиентом (base + len(buf))
	began  bool   // сервер принял header: стрим создан, дальше только resume
	finish writerFinish
	done   bool  // финальный ack (committed/aborted) получен
	err    error // терминальная ошибка записи

	wake chan struct{}
}

func (w *grpcWriter) notify() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *grpcWriter) Write(p []byte) (int, error) {
	written := 0

	w.mu.Lock()
	defer w.mu.Unlock()

	for written < len(p) {
		if w.err != nil {
			return written, w.err
		}
		if w.finish != finishNone {
			return written, errors.New("write after commit/abort")
		}

		free := writeBufferCap - len(w.buf)
		if free <= 0 {
			// backpressure: ждём ack'ов сервера (или реконнекта за ними)
			w.cond.Wait()
			continue
		}

		n := min(free, len(p)-written)
		w.buf = append(w.buf, p[written:written+n]...)
		w.next += int64(n)
		written += n
		w.notify()
	}

	return written, nil
}

// Commit дожидается подтверждения сервером всех данных и фиксации артефакта.
func (w *grpcWriter) Commit() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.err != nil {
		return w.err
	}
	if w.finish == finishNone {
		w.finish = finishCommit
		w.notify()
	}

	for !w.done && w.err == nil {
		w.cond.Wait()
	}
	return w.err
}

// Abort инвалидирует артефакт: follow-читатели получат ошибку. Best-effort:
// ждёт подтверждения ограниченно — брошенный стрим доabort'ит FinishAttempt.
// Идемпотентен: после Commit/Abort — no-op.
func (w *grpcWriter) Abort() error {
	w.mu.Lock()
	if w.done || w.err != nil || w.finish == finishCommit {
		w.mu.Unlock()
		return nil
	}
	if w.finish == finishNone {
		w.finish = finishAbort
		w.notify()
	}
	w.mu.Unlock()

	deadline := time.AfterFunc(abortWaitTimeout, w.cancel)
	defer deadline.Stop()

	w.mu.Lock()
	defer w.mu.Unlock()
	for !w.done && w.err == nil {
		w.cond.Wait()
	}

	if w.err != nil && !errors.Is(w.err, streamstore.ErrAborted) &&
		!errors.Is(w.err, streamstore.ErrNotWriting) && !errors.Is(w.err, streamstore.ErrAttemptFinished) &&
		!errors.Is(w.err, context.Canceled) {
		return w.err
	}
	return nil
}

// run — цикл отправщика: жизненные циклы стрима с реконнектом между ними.
func (w *grpcWriter) run() {
	delay := reconnectMinDelay
	for {
		handshake, err := w.pump()
		if err == nil {
			return // финальный ack получен
		}
		if handshake {
			delay = reconnectMinDelay
		}

		switch {
		case w.ctx.Err() != nil:
			w.fail(err)
			return
		case errors.Is(err, errResumeRetry):
			continue
		case terminalStreamErr(err):
			if w.resolveFinish(err) {
				return // commit/abort прошёл, потерялся только ack
			}
			w.fail(decodeGrpcErr(err))
			return
		}

		select {
		case <-time.After(delay):
		case <-w.ctx.Done():
			w.fail(w.ctx.Err())
			return
		}
		delay = min(delay*2, reconnectMaxDelay)
	}
}

// pump — один жизненный цикл стрима: handshake (header → ack с точкой
// продолжения), отправка неподтверждённого хвоста, приём ack'ов, финальный
// commit/abort. handshake=true — сервер принял header (для сброса backoff).
func (w *grpcWriter) pump() (bool, error) {
	stream, err := w.client.WriteArtifact(w.ctx)
	if err != nil {
		return false, err
	}

	w.mu.Lock()
	resume := w.began
	w.mu.Unlock()

	if err = stream.Send(&pb.WriteArtifactRequest{Msg: &pb.WriteArtifactRequest_Header{
		Header: &pb.WriteArtifactHeader{Ref: encodeGrpcRef(w.ref), Resume: resume},
	}}); err != nil {
		if _, recvErr := stream.Recv(); recvErr != nil {
			err = recvErr
		}
		return false, w.beginConflict(resume, err)
	}

	ack, err := stream.Recv()
	if err != nil {
		return false, w.beginConflict(resume, err)
	}
	if err = w.syncTo(ack.GetSize()); err != nil {
		w.fail(err)
		return true, err
	}

	// приёмник ack'ов: двигает подтверждённую границу буфера
	type recvResult struct {
		final bool
		err   error
	}
	recvCh := make(chan recvResult, 1)
	go func() {
		for {
			ack, err := stream.Recv()
			if err != nil {
				recvCh <- recvResult{err: err}
				return
			}
			if ack.GetCommitted() || ack.GetAborted() {
				w.setDone()
				recvCh <- recvResult{final: true}
				return
			}
			w.applyAck(ack.GetSize())
		}
	}()

	sent := w.ackedSize()
	finalSent := false

	for {
		for {
			chunk, at := w.pendingChunk(sent)
			if len(chunk) == 0 {
				break
			}
			if err = stream.Send(&pb.WriteArtifactRequest{Msg: &pb.WriteArtifactRequest_Chunk{Chunk: chunk}}); err != nil {
				r := <-recvCh // настоящий статус — из Recv
				return true, r.err
			}
			sent = at + int64(len(chunk))
		}

		if fin := w.finishReady(sent); fin != finishNone && !finalSent {
			msg := &pb.WriteArtifactRequest{Msg: &pb.WriteArtifactRequest_Commit{Commit: true}}
			if fin == finishAbort {
				msg = &pb.WriteArtifactRequest{Msg: &pb.WriteArtifactRequest_Abort{Abort: true}}
			}
			if err = stream.Send(msg); err != nil {
				r := <-recvCh
				return true, r.err
			}
			finalSent = true
		}

		select {
		case r := <-recvCh:
			if r.final {
				return true, nil
			}
			return true, r.err
		case <-w.wake:
		case <-w.ctx.Done():
			return true, w.ctx.Err()
		}
	}
}

// beginConflict — ALREADY_EXISTS на первом header: BeginWrite мог пройти на
// сервере до обрыва (ack потерялся) — повторяем handshake в resume-режиме.
func (w *grpcWriter) beginConflict(resume bool, err error) error {
	if !resume && status.Code(err) == codes.AlreadyExists {
		w.mu.Lock()
		w.began = true
		w.mu.Unlock()
		return fmt.Errorf("%w: %w", errResumeRetry, err)
	}
	return err
}

// resolveFinish уточняет исход при терминальной ошибке во время commit/
// abort: финализация могла пройти на сервере, а подтверждение — потеряться
// в обрыве. Правду спрашиваем Stat'ом.
func (w *grpcWriter) resolveFinish(pumpErr error) bool {
	w.mu.Lock()
	fin := w.finish
	sentAll := w.base == w.next
	w.mu.Unlock()

	if fin == finishNone {
		return false
	}
	// для commit все данные должны были дойти; abort'у хватает любого исхода
	if fin == finishCommit && !sentAll {
		return false
	}

	rep, err := w.client.StatArtifact(w.ctx, &pb.StatArtifactRequest{Ref: encodeGrpcRef(w.ref)})
	if err != nil {
		return false
	}

	switch {
	case fin == finishCommit && rep.GetState() == pb.ArtifactState_ARTIFACT_STATE_COMMITTED && rep.GetSize() == w.next:
		w.setDone()
		return true
	case fin == finishAbort && rep.GetState() == pb.ArtifactState_ARTIFACT_STATE_ABORTED:
		w.setDone()
		return true
	default:
		_ = pumpErr
		return false
	}
}

// syncTo синхронизирует буфер с точкой продолжения сервера (ack на header).
func (w *grpcWriter) syncTo(size int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if size < w.base || size > w.next {
		return fmt.Errorf("artifact writer out of sync: server at %d, acked %d, written %d", size, w.base, w.next)
	}

	w.buf = w.buf[size-w.base:]
	w.base = size
	w.began = true
	w.cond.Broadcast()

	return nil
}

// applyAck отбрасывает подтверждённый префикс буфера и будит блокированный
// Write.
func (w *grpcWriter) applyAck(size int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	drop := min(size-w.base, int64(len(w.buf)))
	if drop <= 0 {
		return
	}
	w.buf = w.buf[drop:]
	w.base += drop
	w.cond.Broadcast()
}

func (w *grpcWriter) ackedSize() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.base
}

// pendingChunk — копия очередного неотправленного куска буфера начиная с
// sent (но не раньше подтверждённой границы) и его позиция.
func (w *grpcWriter) pendingChunk(sent int64) ([]byte, int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	at := max(sent, w.base)
	if at >= w.next {
		return nil, at
	}

	n := min(w.next-at, writeChunkSize)
	chunk := make([]byte, n)
	copy(chunk, w.buf[at-w.base:])

	return chunk, at
}

// finishReady — запрошенная финализация, когда весь буфер отправлен.
func (w *grpcWriter) finishReady(sent int64) writerFinish {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.finish == finishNone || sent < w.next {
		return finishNone
	}
	return w.finish
}

func (w *grpcWriter) setDone() {
	w.mu.Lock()
	w.done = true
	w.mu.Unlock()
	w.cond.Broadcast()
}

func (w *grpcWriter) fail(err error) {
	if err == nil {
		err = errors.New("artifact write failed")
	}
	w.mu.Lock()
	w.err = decodeGrpcErr(err)
	w.mu.Unlock()
	w.cond.Broadcast()
}

// ── Чтение ──────────────────────────────────────────────

// grpcReader адаптирует серверный стрим чтения к io.ReadCloser. Обрыв
// соединения переживается реконнектом: стрим переоткрывается с текущего
// offset'а, читатель обрыва не замечает.
type grpcReader struct {
	client pb.ArtifactServiceClient
	ref    ArtifactRef
	ctx    context.Context
	cancel context.CancelFunc

	stream grpc.ServerStreamingClient[pb.ReadArtifactResponse]
	off    int64
	delay  time.Duration
	buf    []byte // недочитанный хвост текущего chunk'а
	err    error
}

func (r *grpcReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		if r.err != nil {
			return 0, r.err
		}

		if r.stream == nil {
			stream, err := r.client.ReadArtifact(r.ctx, &pb.ReadArtifactRequest{
				Ref: encodeGrpcRef(r.ref), Offset: r.off, Follow: true,
			})
			if err != nil {
				if !r.backoff(err) {
					return 0, r.err
				}
				continue
			}
			r.stream = stream
		}

		msg, err := r.stream.Recv()
		switch {
		case err == nil:
			r.buf = msg.GetChunk()
			r.off += int64(len(r.buf))
			r.delay = 0
		case errors.Is(err, io.EOF):
			r.err = io.EOF // артефакт дочитан
		default:
			if !r.backoff(err) {
				return 0, r.err
			}
			r.stream = nil // реконнект с текущего offset'а
		}
	}

	n := copy(p, r.buf)
	r.buf = r.buf[n:]

	return n, nil
}

// backoff решает судьбу ошибки стрима: транзиентная — пауза перед
// реконнектом (true), терминальная — фиксируется в r.err (false).
func (r *grpcReader) backoff(err error) bool {
	if r.ctx.Err() != nil || terminalStreamErr(err) {
		r.err = decodeGrpcErr(err)
		return false
	}

	if r.delay == 0 {
		r.delay = reconnectMinDelay
	}
	select {
	case <-time.After(r.delay):
	case <-r.ctx.Done():
		r.err = r.ctx.Err()
		return false
	}
	r.delay = min(r.delay*2, reconnectMaxDelay)

	return true
}

func (r *grpcReader) Close() error {
	r.cancel()
	return nil
}

// decodeGrpcErr маппит статусы artifact-сервера обратно в ошибки
// streamstore: код и поведение remote-стора совпадают с локальным.
// FAILED_PRECONDITION делится по тексту — сервер кладёт в message
// sentinel-ошибку домена.
func decodeGrpcErr(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}

	st, ok := status.FromError(err)
	if !ok {
		return err
	}

	switch st.Code() {
	case codes.NotFound:
		return fmt.Errorf("%w: %s", streamstore.ErrNotFound, st.Message())
	case codes.AlreadyExists:
		return fmt.Errorf("%w: %s", streamstore.ErrAlreadyExists, st.Message())
	case codes.Aborted:
		return fmt.Errorf("%w: %s", streamstore.ErrAborted, st.Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", streamstore.ErrInvalidRef, st.Message())
	case codes.FailedPrecondition:
		if strings.Contains(st.Message(), streamstore.ErrAttemptFinished.Error()) {
			return fmt.Errorf("%w: %s", streamstore.ErrAttemptFinished, st.Message())
		}
		return fmt.Errorf("%w: %s", streamstore.ErrNotWriting, st.Message())
	default:
		return err
	}
}
