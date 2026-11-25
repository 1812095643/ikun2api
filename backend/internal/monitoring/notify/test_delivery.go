package notify

import (
	"context"
	"errors"
	"time"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/google/uuid"
)

// 仅管理员明确点“测试”时调用；记录实际平台回执，不向事件列表伪造故障。
func (w *Worker) Test(ctx context.Context, channelID, targetID string) (string, error) {
	t, err := w.Store.Target(ctx, targetID)
	if err != nil || t.ChannelID != channelID {
		return "dead", &Failure{Class: "target", Permanent: true}
	}
	c, secrets, err := w.Store.ChannelSecret(ctx, channelID, w.Cipher)
	if err != nil {
		return "dead", &Failure{Class: "credentials", Permanent: true}
	}
	defer clear(secrets)
	release, ok, err := w.Store.LockChannel(ctx, channelID)
	if err != nil || !ok {
		return "dead", &Failure{Class: "busy"}
	}
	defer release()
	cfg, err := w.Store.Config(ctx)
	if err != nil {
		return "dead", err
	}
	if cfg.DetailBaseURL == "" && w.DetailsURL != nil {
		cfg.DetailBaseURL = w.DetailsURL(ctx)
	}
	now := time.Now().UTC()
	d := domain.Delivery{ID: uuid.NewString(), IncidentID: "test", Revision: 1, Channel: channelID, Target: targetID, Kind: "test", State: "leased", NextAttempt: now, LeaseUntil: now.Add(2 * time.Minute), LeaseToken: uuid.NewString(), Attempts: 1}
	if err = w.Store.InsertTestDelivery(ctx, d); err != nil {
		return "dead", err
	}
	i := domain.Incident{ID: d.ID, Rule: "test", Status: "test", Severity: "P2", LastSeen: now, Explanation: domain.Explanation{Conclusion: "通知通道测试"}}
	m := BuildMessage(cfg, i, "test", false)
	m.Title = "通知通道测试"
	m.Text = "这是一条管理员发起的测试通知。"
	if err = w.Store.MarkDeliverySending(ctx, d, now); err != nil {
		return "dead", err
	}
	r, sendErr := w.Sender.Send(ctx, c, secrets, t, m, "")
	d.State = "accepted"
	d.MessageID = r.MessageID
	d.AcceptedAt = time.Now().UTC()
	if sendErr != nil {
		d.State = "dead"
		var f *Failure
		if errors.As(sendErr, &f) {
			d.ErrorClass = f.Class
			if f.Uncertain {
				d.State = "uncertain"
			}
		} else {
			d.ErrorClass = "network"
		}
	}
	if err = w.Store.CompleteDelivery(ctx, d); err != nil {
		return d.State, err
	}
	return d.State, sendErr
}
