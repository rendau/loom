package model

import "time"

// Роли пользователей админки.
const (
	RoleAdmin = "admin" // всё, включая глобальные переменные/секреты и users
	RoleUser  = "user"  // чтение всего + изменение назначенных дагов
)

// SessionTTL — срок жизни сессии.
const SessionTTL = 30 * 24 * time.Hour

// Ограничения пароля: bcrypt работает максимум с 72 байтами.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 72
)

// Main — пользователь админки. DagNames заполняется отдельным запросом
// (назначенные даги); у admin всегда пусто — ему доступны все.
type Main struct {
	Id         string
	Username   string
	Role       string
	DagNames   []string
	CreatedAt  time.Time
	ModifiedAt time.Time // zero — не изменялся
}

// CreateSpec — создание пользователя.
type CreateSpec struct {
	Username string
	Password string
	Role     string
	DagNames []string
}

// UpdateSpec — частичное изменение; nil-поля не трогаются, SetDagNames
// отличает «не менять» от «очистить набор».
type UpdateSpec struct {
	Password    *string
	Role        *string
	DagNames    []string
	SetDagNames bool
}

// AuthInfo — аутентифицированный вызывающий (кладётся в контекст).
type AuthInfo struct {
	UserId   string
	Username string
	Role     string
}

func (a AuthInfo) IsAdmin() bool { return a.Role == RoleAdmin }
