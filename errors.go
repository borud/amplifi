package amplifi

import (
	"errors"
	"strconv"
)

// ErrAuth is returned by Dial when the router rejects the login, typically
// because the password is wrong. Test for it with errors.Is.
var ErrAuth = errors.New("authentication failed")

// CertificateMismatchError is returned by Dial when the unit's certificate
// does not have the fingerprint given with WithCertificateFingerprint. Test
// for it with errors.As.
type CertificateMismatchError struct {
	Want string
	Got  string
}

// Error formats the error with both fingerprints.
func (e *CertificateMismatchError) Error() string {
	return "certificate fingerprint mismatch: want " + e.Want + ", got " + e.Got
}

// RouterError is an error reported by the router in a response's
// payload.error. Test for it with errors.As.
type RouterError struct {
	Code int
	Msg  string
}

// Error formats the error as "router error <code>: <msg>".
func (e *RouterError) Error() string {
	return "router error " + strconv.Itoa(e.Code) + ": " + e.Msg
}

// IsRouterError reports whether err is, or wraps, a RouterError: the router
// understood the request and refused it, as opposed to a transport failure.
func IsRouterError(err error) bool {
	_, ok := errors.AsType[*RouterError](err)
	return ok
}
