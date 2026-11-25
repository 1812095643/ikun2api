package detectors

import (
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"strings"
	"testing"
	"time"
)

func TestSignatureAttributionSessionDedupAndIndependentErrors(t *testing.T) {
	now := time.Now()
	s := observation("signature.rejected", now)
	s.Reason = "thinking_signature_invalid"
	s.TaskID = "session:abc"
	s.Metrics = map[string]float64{"signature_session_known": 1, "signature_forward_unchanged": 1, "signature_matches_previous": 1}
	db := s
	db.Source = "postgres.errors"
	db.EventID = "db"
	db.Kind = "request.error"
	db.TaskID = ""
	db.Metrics = nil
	db.UpstreamStatus = 400
	generic := observation("attempt.failed", now)
	generic.UpstreamStatus = 400
	fs := Evaluate([]domain.Observation{db, generic, s}, now, domain.DefaultConfig())
	if len(fs) != 1 || fs[0].Rule != "F02" || !strings.Contains(fs[0].Explanation.Conclusion, "原样回传") {
		t.Fatalf("%+v", fs)
	}
	s2 := s
	s2.RequestID = "r2"
	s2.EventID = "r2"
	s2.AccountID = 99
	more := Evaluate([]domain.Observation{s, s2}, now, domain.DefaultConfig())
	if len(more) != 2 || more[0].Fingerprint != more[1].Fingerprint {
		t.Fatal(more)
	}
	s2.TaskID = "session:other"
	more = Evaluate([]domain.Observation{s, s2}, now, domain.DefaultConfig())
	if len(more) != 2 || more[0].Fingerprint == more[1].Fingerprint {
		t.Fatal(more)
	}
	generic.UpstreamStatus = 429
	more = Evaluate([]domain.Observation{s, generic}, now, domain.DefaultConfig())
	if !hasRule(more, "U01") || !hasRule(more, "F02") {
		t.Fatal(more)
	}
}
func TestSignatureMissingSessionAndDatabaseArrivalGrace(t *testing.T) {
	now := time.Now()
	o := observation("request.error", now)
	o.Source = "postgres.errors"
	o.Reason = "thinking_signature_invalid"
	o.UpstreamStatus = 400
	if got := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig()); len(got) != 0 {
		t.Fatal(got)
	}
	got := Evaluate([]domain.Observation{o}, now.Add(31*time.Second), domain.DefaultConfig())
	if len(got) != 1 || !strings.Contains(got[0].Scope, "unknown") || got[0].Explanation.Conclusion != "会话历史思考签名无效（损坏环节未确认）" {
		t.Fatal(got)
	}
	o2 := o
	o2.RequestID = "r2"
	o2.EventID = "2"
	o2.UserID = 20
	got = Evaluate([]domain.Observation{o, o2}, now.Add(time.Minute), domain.DefaultConfig())
	if len(got) != 2 || got[0].Fingerprint == got[1].Fingerprint {
		t.Fatal(got)
	}
}

func TestSignatureMissingRequestIDStillClassified(t *testing.T) {
	now := time.Now()
	o := observation("request.error", now)
	o.RequestID = ""
	o.Reason = "thinking_signature_invalid"
	o.UpstreamStatus = 400
	fs := Evaluate([]domain.Observation{o}, now, domain.DefaultConfig())
	if len(fs) != 1 || fs[0].Rule != "F02" {
		t.Fatal(fs)
	}
}

func TestSignatureDoesNotMaskLaterIndependentFailure(t *testing.T) {
	now := time.Now()
	s := observation("signature.rejected", now)
	s.Reason = "thinking_signature_invalid"
	end := observation("request.finished", now)
	end.Outcome = "failed"
	end.ClientStatus = 502
	fs := Evaluate([]domain.Observation{s, end}, now, domain.DefaultConfig())
	if !hasRule(fs, "F01") || !hasRule(fs, "F02") {
		t.Fatal(fs)
	}
}
