package errs

import "errors"

type Err string

func (e Err) Error() string {
	return string(e)
}

// common errors
const (
	ServiceNA         = Err("service_not_available")
	NotAuthorized     = Err("not_authorized")
	ObjectNotFound    = Err("object_not_found")
	InvalidRequest    = Err("invalid_request")
	IncorrectPageSize = Err("incorrect_page_size")
	IdRequired        = Err("id_required")
)

// domain errors
const (
	DagNotFound          = Err("dag_not_found")
	DagExists            = Err("dag_exists")
	ProjectNotFound      = Err("project_not_found")
	TemplateNotFound     = Err("template_not_found")
	RegistrationNotFound = Err("registration_not_found")
	RunNotFound          = Err("run_not_found")
	RunNotFinished       = Err("run_not_finished")
	RunNotRunning        = Err("run_not_running")
	TaskNotFound         = Err("task_not_found")
	TaskNotRetryable     = Err("task_not_retryable")
	InvalidManifest      = Err("invalid_manifest")
	ImageRequired        = Err("image_required")
	AttemptNotFound      = Err("attempt_not_found")
	AttemptOutdated      = Err("attempt_outdated")
	PoolNotFound         = Err("pool_not_found")
	SecretNotFound       = Err("secret_not_found")
	VariableNotFound     = Err("variable_not_found")
	UserNotFound         = Err("user_not_found")
	UserExists           = Err("user_exists")
	InvalidCredentials   = Err("invalid_credentials")
	PermissionDenied     = Err("permission_denied")
	ValueNotFound        = Err("value_not_found")
	LogAlreadyPushed     = Err("log_already_pushed")
	AttemptLogAborted    = Err("attempt_log_aborted")
	ArtifactNotFound     = Err("artifact_not_found")
	ArtifactAborted      = Err("artifact_aborted")
)

type ErrFull struct {
	Err    error
	Desc   string
	Fields map[string]string
}

func (e ErrFull) Error() string {
	return e.Err.Error() + ", desc: " + e.Desc
}

func (e ErrFull) Unwrap() error {
	return e.Err
}

// AsErr извлекает Err из цепочки ошибок — и когда она вернулась значением
// (Err), и когда указателем (*Err).
func AsErr(err error) (Err, bool) {
	if e, ok := errors.AsType[Err](err); ok {
		return e, true
	}
	if e, ok := errors.AsType[*Err](err); ok && e != nil {
		return *e, true
	}
	return "", false
}

// AsErrFull извлекает ErrFull из цепочки ошибок — значением или указателем.
func AsErrFull(err error) (ErrFull, bool) {
	if e, ok := errors.AsType[ErrFull](err); ok {
		return e, true
	}
	if e, ok := errors.AsType[*ErrFull](err); ok && e != nil {
		return *e, true
	}
	return ErrFull{}, false
}
