package maintenance

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestSendSMTPInvalidServerHasClearError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	err = SendSMTP(context.Background(), SMTPConfig{
		Host: "127.0.0.1", Port: port, TLS: "none",
		From: "devbox@example.com", To: "ops@example.com",
	}, "test", "body")
	if err == nil || !strings.Contains(err.Error(), "SMTP connection to 127.0.0.1:"+strconv.Itoa(port)+" failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSMTPRejectsInvalidTLS(t *testing.T) {
	err := ValidateSMTP(SMTPConfig{Host: "smtp.example.com", Port: 25, TLS: "maybe", From: "a@example.com", To: "b@example.com"})
	if err == nil || !strings.Contains(err.Error(), "TLS mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}
