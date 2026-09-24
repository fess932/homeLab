package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/fess932/homeLab/internal/netguard"
)

const (
	KindDNS        = "dns"
	KindConnect    = "connect"
	KindTLS        = "tls"
	KindTimeout    = "timeout"
	KindHTTPStatus = "http_status"
	KindForbidden  = "forbidden_address"
)

func Classify(err error) (kind, msg string) {
	if err == nil {
		return "", ""
	}
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var unknownAuth x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invalidErr x509.CertificateInvalidError
	var recordErr tls.RecordHeaderError
	var alertErr tls.AlertError
	switch {
	case errors.Is(err, netguard.ErrForbidden):
		if fe, ok := errors.AsType[*netguard.ForbiddenError](err); ok {
			return KindForbidden, fe.Error()
		}
		return KindForbidden, "адрес запрещён политикой исходящих соединений"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded), isTimeout(err):
		return KindTimeout, "превышено время ожидания"
	case errors.As(err, &dnsErr):
		if dnsErr.IsNotFound {
			return KindDNS, "имя " + dnsErr.Name + " не найдено в DNS"
		}
		return KindDNS, "ошибка DNS: " + dnsErr.Err
	case errors.As(err, &certErr), errors.As(err, &unknownAuth), errors.As(err, &hostErr), errors.As(err, &invalidErr):
		return KindTLS, "ошибка TLS-сертификата: " + innermost(err)
	case errors.As(err, &recordErr), errors.As(err, &alertErr), strings.Contains(err.Error(), "tls:"):
		return KindTLS, "ошибка TLS: " + innermost(err)
	default:
		return KindConnect, "ошибка соединения: " + innermost(err)
	}
}

func isTimeout(err error) bool {
	ne, ok := errors.AsType[net.Error](err)
	return ok && ne.Timeout()
}

func innermost(err error) string {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		err = ue.Err
	}
	if oe, ok := errors.AsType[*net.OpError](err); ok && oe.Err != nil {
		err = oe.Err
	}
	return err.Error()
}
