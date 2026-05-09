package errors

const (
	TypeError                = "error"
	TypeModelNotFound        = "model_not_found"
	TypeRequestRetryExceeded = "request_retry_exceeded"
)

const (
	MessageError                            = "error"
	MessageRuntimeNotReady                  = "runtime not ready"
	MessageWrongNumberOfArgumentsGet        = "wrong number of arguments for 'get' command"
	MessageWrongNumberOfArgumentsRPOP       = "wrong number of arguments for 'rpop' command"
	MessageInvalidRequestJSON               = "invalid request json"
	MessageUnsupportedType                  = "unsupported type"
	MessageMissingAuthIndex                 = "missing auth_index"
	MessageAuthNotFound                     = "auth not found"
	MessageNoDispatchResult                 = "no dispatch result"
	MessageNoAuthAvailable                  = "no auth available"
	MessageMissingModel                     = "missing model"
	MessageMissingRequiredCredentialHeaders = "missing required credential headers"
	MessageInvalidAPIKey                    = "invalid api key"
	MessageRequestRetryExceeded             = "request retry limit exceeded"

	MessageModelDoesNotExistFmt = "model %s does not exist"
)
