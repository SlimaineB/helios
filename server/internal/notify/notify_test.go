package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendSignsPayload(t *testing.T) {
	var got []byte
	var sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		sig = r.Header.Get("X-AnsibleHub-Signature")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := &Notifier{url: srv.URL, secret: "s3cret", client: srv.Client()}
	if err := n.SendSync(context.Background(), Event{Event: "run_failed", AppName: "web"}); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("empty payload")
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	_, _ = mac.Write(got)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig != want {
		t.Fatalf("signature = %q, want %q", sig, want)
	}
}

func TestDisabledNotifierIsNoop(t *testing.T) {
	var n *Notifier
	n.Send(Event{Event: "x"})
	if err := n.SendSync(context.Background(), Event{}); err != nil {
		t.Fatal(err)
	}
}
