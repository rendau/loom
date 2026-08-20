package pg

import (
	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mechta-market/mobone/v2"
)

type Base struct {
	Con *pgxpool.Pool
	TxM *mobone.TransactionManager
	QB  squirrel.StatementBuilderType
}

func NewBase(con *pgxpool.Pool, txm *mobone.TransactionManager) *Base {
	return &Base{
		Con: con,
		TxM: txm,
		QB:  squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}
