package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// userErr — ошибка, которую видит пользователь. В UI она уходит ключом i18n,
// а не готовой строкой: иначе при немецком или английском интерфейсе всплывают
// русские фразы («Скачивание», «Распаковка», «Готово» — это и была жалоба).
type userErr struct {
	Key    string
	Detail string
}

func (e *userErr) Error() string {
	if e.Detail != "" {
		return e.Key + ": " + e.Detail
	}
	return e.Key
}

// uerr собирает локализуемую ошибку: ключ + техническая подробность (HTTP-код,
// имя ассета, текст от ОС). Подробность показывается мелким шрифтом.
func uerr(key, detail string) error { return &userErr{Key: key, Detail: detail} }

// errKeyOf достаёт ключ из ошибки. Всё, что не размечено, помечается generic +
// исходный текст, чтобы пользователь видел хоть что-то осмысленное.
func errKeyOf(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	var ue *userErr
	if errors.As(err, &ue) {
		return ue.Key, ue.Detail
	}
	return "err.generic", strings.TrimSpace(err.Error())
}

// errHTTP переводит код ответа в ключ сообщения.
func errHTTP(status int) error {
	switch {
	case status == http.StatusForbidden || status == http.StatusTooManyRequests:
		return uerr("err.rateLimit", fmt.Sprintf("HTTP %d", status))
	case status == http.StatusNotFound:
		return uerr("err.assetMissing", fmt.Sprintf("HTTP %d", status))
	case status >= 400:
		return uerr("err.httpStatus", fmt.Sprintf("HTTP %d", status))
	}
	return nil
}

// errNet размечает сетевые сбои: таймаут, DNS, обрыв отвечают разными фразами.
func errNet(err error) error {
	if err == nil {
		return nil
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return uerr("err.timeout", "")
	}
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "no such host"):
		return uerr("err.dns", "")
	case strings.Contains(low, "connection refused"), strings.Contains(low, "connection reset"):
		return uerr("err.network", "")
	case strings.Contains(low, "context canceled"):
		return uerr("err.canceled", "")
	}
	return uerr("err.network", strings.TrimSpace(err.Error()))
}
