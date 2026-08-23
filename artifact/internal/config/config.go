package config

import (
	"github.com/caarlos0/env/v9"
	_ "github.com/joho/godotenv/autoload"
)

var Conf = struct {
	Debug    bool   `env:"DEBUG" envDefault:"false"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	GrpcPort       string `env:"GRPC_PORT" envDefault:"5050"`
	SystemHttpPort string `env:"SYSTEM_HTTP_PORT" envDefault:"3003"`

	DataDir string `env:"DATA_DIR" envDefault:"./data"`

	// AuthSecret — общий с control plane секрет attempt-токенов; пусто —
	// проверка токенов выключена (dev).
	AuthSecret string `env:"AUTH_SECRET"`
}{}

func init() {
	if err := env.Parse(&Conf); err != nil {
		panic(err)
	}
}
