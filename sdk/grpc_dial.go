package loom

import (
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

// Реконнект оборванных стримов (лог-синк, писатель/читатель артефактов):
// первая пауза короткая, потолок — пара секунд, чтобы вернувшийся после
// рестарта сервер подхватывался почти сразу.
const (
	reconnectMinDelay = 200 * time.Millisecond
	reconnectMaxDelay = 3 * time.Second
)

// terminalStreamErr — сервер отказал по существу (завершённая попытка,
// конфликт записи, невалидный запрос, ошибка диска): реконнект стрима не
// поможет. Транспортные обрывы (UNAVAILABLE и прочие сетевые) — не
// терминальны, их лечит реконнект.
func terminalStreamErr(err error) bool {
	switch status.Code(err) {
	case codes.FailedPrecondition, codes.InvalidArgument, codes.NotFound,
		codes.Aborted, codes.AlreadyExists, codes.Unimplemented, codes.Internal:
		return true
	default:
		return false
	}
}

// dialOpts — общие настройки всех gRPC-подключений SDK (artifact-сервер и
// control plane): быстрое обнаружение мёртвого соединения и быстрый
// реконнект. Рестарт сервера не должен стоить таску больше нескольких
// секунд ожидания.
//
//   - keepalive: ping раз в 10s (минимум клиентского gRPC), обрыв — через
//     Timeout без ответа; Timeout также становится TCP_USER_TIMEOUT (Linux),
//     поэтому активная отправка замечает молчащий канал за ~Timeout;
//   - reconnect backoff: с 200ms до потолка 3s — упавший сервер
//     переподключается почти сразу после возвращения;
//   - unary-вызовы ретраятся на UNAVAILABLE самим каналом (service config);
//     стримы переподключают свои циклы (лог-синк, писатель/читатель
//     артефактов).
func dialOpts() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  200 * time.Millisecond,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   3 * time.Second,
			},
			MinConnectTimeout: 3 * time.Second,
		}),
		grpc.WithDefaultServiceConfig(retryServiceConfig),
	}
}

// retryServiceConfig — ретрай unary-вызовов на UNAVAILABLE (рестарт/обрыв
// сервера): PushValue/PullValue, FinishAttempt, Stat и прочие одиночные
// вызовы переживают рестарт без ошибки в коде таска.
const retryServiceConfig = `{
  "methodConfig": [{
    "name": [{}],
    "retryPolicy": {
      "maxAttempts": 5,
      "initialBackoff": "0.2s",
      "maxBackoff": "3s",
      "backoffMultiplier": 2,
      "retryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}`
