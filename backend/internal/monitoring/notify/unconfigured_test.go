package notify

import (
	"context"
	"errors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"net/http"
	"testing"
)

type forbiddenNetwork struct{ t *testing.T }

func (f forbiddenNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Fatal("blank credentials must not make network requests")
	return nil, errors.New("forbidden")
}
func TestBlankFeishuConnectionCannotClaimDelivery(t *testing.T) {
	f := NewFeishuWithTransport("", "", forbiddenNetwork{t})
	id, err := f.Send(context.Background(), domain.Delivery{Target: "oc_fixture"}, "", "")
	var failure *Failure
	if id != "" || !errors.As(err, &failure) || failure.Class != "credentials_missing" {
		t.Fatalf("blank config was not rejected: id=%q error=%v", id, err)
	}
}
