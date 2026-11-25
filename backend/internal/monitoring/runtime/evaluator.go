// 采集不完整时停止恢复判定，避免把缺失证据当作业务正常。
package runtime

import (
	"context"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/monitoring/detectors"
	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
	"github.com/MACOS-DO/sub4api/internal/monitoring/incidents"
	"github.com/MACOS-DO/sub4api/internal/monitoring/store"
	"strings"
	"time"
)

type Evaluator struct {
	Report        func(string, time.Time, error)
	Store         *store.Store
	Manager       *incidents.Manager
	Clock         Clock
	lastRetention time.Time
}

func (e *Evaluator) Poll(ctx context.Context) (result error) {
	defer func() {
		if e.Report != nil {
			e.Report("evaluation", e.Clock.Now(), result)
		}
	}()
	now := e.Clock.Now()
	cfg, err := e.Store.Config(ctx)
	if err != nil {
		return err
	}
	var checkpoint struct {
		Sequence  int64     `json:"sequence"`
		TaskAfter string    `json:"task_after"`
		TaskCycle time.Time `json:"task_cycle"`
	}
	if err = e.Store.State(ctx, "evaluation", &checkpoint); err != nil {
		return err
	}
	incoming, last, err := e.Store.Page(ctx, checkpoint.Sequence, 5000)
	if err != nil {
		return err
	}
	window, truncated, err := e.Store.Window(ctx, now.Add(-20*time.Minute), 20000)
	if err != nil {
		return err
	}
	prioritySet := map[string]bool{}
	priority := []string{}
	consider := func(o domain.Observation) {
		if store.IsTaskFact(o.Kind) && o.TaskID != "" && len(priority) < 1000 {
			key := store.TaskKey(o)
			if !prioritySet[key] {
				prioritySet[key] = true
				priority = append(priority, key)
			}
		}
	}
	for _, o := range incoming {
		consider(o)
	}
	for n := len(window) - 1; n >= 0; n-- {
		consider(window[n])
	}
	tasks, nextTask, err := e.Store.TaskPage(ctx, checkpoint.TaskAfter, 500, priority)
	if err != nil {
		return err
	}
	selectedTasks := map[string]bool{}
	for _, o := range tasks {
		selectedTasks[store.TaskKey(o)] = true
	}
	seen := map[string]bool{}
	all := make([]domain.Observation, 0, len(window)+len(incoming)+len(tasks))
	for _, rows := range [][]domain.Observation{incoming, window, tasks} {
		for _, o := range rows {
			if store.IsTaskFact(o.Kind) && o.TaskID != "" && !selectedTasks[store.TaskKey(o)] {
				continue
			}
			key := o.Source + ":" + o.EventID
			if !seen[key] {
				seen[key] = true
				all = append(all, o)
			}
		}
	}
	state := "source.recovered"
	reason := "bounded_window_complete"
	if truncated {
		state = "source.gap"
		reason = "evaluation_window_truncated"
	}
	all = append(all, domain.Observation{Schema: 1, Site: cfg.Site, Source: "monitor", EventID: fmt.Sprintf("window:%t:%d", truncated, now.Unix()/30), TaskID: "evaluation", Kind: state, Reason: reason, Quality: domain.Fresh, OccurredAt: now, ObservedAt: now})
	if checkpoint.TaskCycle.IsZero() {
		checkpoint.TaskCycle = now
	}
	taskKind := "source.recovered"
	if now.Sub(checkpoint.TaskCycle) > 2*time.Minute && nextTask != "" {
		taskKind = "source.gap"
	}
	all = append(all, domain.Observation{Schema: 1, Site: cfg.Site, Source: "monitor", EventID: fmt.Sprintf("task-cycle:%s:%d", taskKind, now.Unix()/30), TaskID: "task_evaluation", Kind: taskKind, Reason: "bounded_task_sweep", Quality: domain.Fresh, OccurredAt: now, ObservedAt: now})
	if nextTask == "" {
		checkpoint.TaskCycle = now
	}
	checkpoint.TaskAfter = nextTask
	cursors, err := e.Store.Cursors(ctx)
	if err != nil {
		return err
	}
	for _, c := range cursors {
		staleAfter := 2 * time.Minute
		if c.Source == "notification" {
			continue
		}
		if strings.Contains(c.Source, "reconcile") {
			staleAfter = 2 * time.Hour
		}
		if !c.LastSuccess.IsZero() && now.Sub(c.LastSuccess) > staleAfter {
			all = append(all, domain.Observation{Schema: 1, Site: cfg.Site, Source: "monitor", EventID: fmt.Sprintf("stale:%s:%d", c.Source, now.Unix()/30), Kind: "source.unavailable", TaskID: c.Source, Reason: "stale_heartbeat", Quality: domain.Fresh, OccurredAt: now, ObservedAt: now})
		}
	}
	backlog, err := e.Store.Backlog(ctx, now)
	if err != nil {
		return err
	}
	for _, o := range backlog {
		o.Schema = 1
		o.Site = cfg.Site
		o.Source = "monitor"
		o.EventID = fmt.Sprintf("backlog:%s:%d", o.TaskID, now.Unix()/30)
		o.Quality = domain.Fresh
		o.OccurredAt = now
		o.ObservedAt = now
		all = append(all, o)
	}
	active, err := e.Store.ActiveIncidents(ctx)
	if err != nil {
		return err
	}
	complete := !truncated && incidents.SourcesFresh(cursors, now)
	for _, f := range detectors.Evaluate(all, now, cfg, active...) {
		if f.Healthy && !complete {
			continue
		}
		if truncated && (f.Rule == "L03" || f.Rule == "C01" || f.Rule == "S02" || f.Rule == "R01" && strings.Contains(f.EvidenceID, "switch_ratio")) {
			continue
		}
		if err = e.Manager.Apply(ctx, f, cfg, now); err != nil {
			return err
		}
	}
	if !truncated && incidents.SourcesFresh(cursors, now) {
		if _, err = e.Manager.CloseQuiet(ctx, cfg, now); err != nil {
			return err
		}
	}
	if err = e.Manager.Tick(ctx, cfg, now, complete); err != nil {
		return err
	}
	checkpoint.Sequence = last
	if err = e.Store.SaveState(ctx, "evaluation", checkpoint); err != nil {
		return err
	}
	if now.Sub(e.lastRetention) > time.Hour {
		if err = e.Store.Retain(ctx, now); err != nil {
			return err
		}
		e.lastRetention = now
	}
	return nil
}
func (e *Evaluator) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = e.Poll(ctx)
		}
	}
}
