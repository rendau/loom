package config

import (
	"github.com/caarlos0/env/v9"
	_ "github.com/joho/godotenv/autoload"
)

var Conf = struct {
	Debug    bool   `env:"DEBUG" envDefault:"false"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	GrpcPort       string `env:"GRPC_PORT" envDefault:"5051"`
	SystemHttpPort string `env:"SYSTEM_HTTP_PORT" envDefault:"3003"`

	DataDir string `env:"DATA_DIR" envDefault:"./data"`
}{}

func init() {
	if err := env.Parse(&Conf); err != nil {
		panic(err)
	}
}
