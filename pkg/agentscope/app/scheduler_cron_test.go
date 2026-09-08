package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/schedule"
)

type fakeScheduler struct {
	tasks []*schedule.Task
	fns   []schedule.TaskFunc
	ids   []string
	n     int
}

func (f *fakeScheduler) Schedule(_ context.Context, task *schedule.Task, fn schedule.TaskFunc) (string, error) {
	f.n++
	id := "task-" + string(rune('0'+f.n))
	f.tasks = append(f.tasks, task)
	f.fns = append(f.fns, fn)
	f.ids = append(f.ids, id)
	return id, nil
}
func (f *fakeScheduler) Cancel(_ context.Context, _ string) error                { return nil }
func (f *fakeScheduler) Get(_ context.Context, _ string) (*schedule.Task, error) { return nil, nil }
func (f *fakeScheduler) List(_ context.Context) ([]*schedule.Task, error)        { return nil, nil }
func (f *fakeScheduler) Close() error                                            { return nil }

func TestSchedulerCreateRejectsInvalidCron(t *testing.T) {
	fs := &fakeScheduler{}
	m := NewSchedulerManager(fs, nil)
	_, err := m.Create(context.Background(), CreateScheduleRequest{
		SessionID: "s1", CronExpr: "0 9 * * * *", Input: "hi",
	})
	var verr *CronValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected *CronValidationError, got %v", err)
	}
	if len(fs.tasks) != 0 {
		t.Error("invalid schedule must not reach the scheduler")
	}
	if len(m.List()) != 0 {
		t.Error("invalid schedule must not be persisted")
	}
}

func TestSchedulerCreateRejectsNeverFiringCron(t *testing.T) {
	fs := &fakeScheduler{}
	m := NewSchedulerManager(fs, nil)
	_, err := m.Create(context.Background(), CreateScheduleRequest{
		SessionID: "s1", CronExpr: "0 0 30 2 *", Input: "hi",
	})
	var verr *CronValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected *CronValidationError, got %v", err)
	}
}

func TestSchedulerCreateValidCronUsesNextFire(t *testing.T) {
	fs := &fakeScheduler{}
	m := NewSchedulerManager(fs, nil)
	before := time.Now()
	rec, err := m.Create(context.Background(), CreateScheduleRequest{
		SessionID: "s1", CronExpr: "0 9 * * *", Input: "hi",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Status != "active" {
		t.Errorf("status = %q, want active", rec.Status)
	}
	task := fs.tasks[0]
	if task.Interval != 0 {
		t.Errorf("cron task must not use Interval, got %v", task.Interval)
	}
	// RunAt must be the next 09:00, not "now + 1h" (the old silent fallback).
	if task.RunAt.Hour() != 9 || task.RunAt.Minute() != 0 {
		t.Errorf("RunAt = %v, want next 09:00", task.RunAt)
	}
	if !task.RunAt.After(before) {
		t.Errorf("RunAt %v should be in the future", task.RunAt)
	}
}

func TestSchedulerCronRearmsAfterRun(t *testing.T) {
	fs := &fakeScheduler{}
	m := NewSchedulerManager(fs, nil)
	m.setChatFn(func(_ context.Context, _ string, _ string) error { return nil })
	rec, err := m.Create(context.Background(), CreateScheduleRequest{
		SessionID: "s1", CronExpr: "*/10 * * * *", Input: "hi",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(fs.tasks) != 1 {
		t.Fatalf("expected 1 scheduled task, got %d", len(fs.tasks))
	}
	// Run the captured task function: it must re-arm the next fire.
	if err := fs.fns[0](context.Background(), fs.tasks[0]); err != nil {
		t.Fatalf("task fn: %v", err)
	}
	if len(fs.tasks) != 2 {
		t.Fatalf("expected re-arm schedule, got %d tasks", len(fs.tasks))
	}
	if !fs.tasks[1].RunAt.After(fs.tasks[0].RunAt) {
		t.Errorf("re-armed RunAt %v must be after first %v", fs.tasks[1].RunAt, fs.tasks[0].RunAt)
	}
	updated, _ := m.Get(rec.ID)
	if updated.TaskID != fs.ids[1] {
		t.Errorf("record TaskID = %q, want re-armed id %q", updated.TaskID, fs.ids[1])
	}
}

func TestSchedulerCancelStopsRearm(t *testing.T) {
	fs := &fakeScheduler{}
	m := NewSchedulerManager(fs, nil)
	m.setChatFn(func(_ context.Context, _ string, _ string) error { return nil })
	rec, err := m.Create(context.Background(), CreateScheduleRequest{
		SessionID: "s1", CronExpr: "*/10 * * * *", Input: "hi",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := m.Cancel(context.Background(), rec.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// A run already in flight when cancel lands must not re-arm.
	_ = fs.fns[0](context.Background(), fs.tasks[0])
	if len(fs.tasks) != 1 {
		t.Errorf("canceled schedule re-armed: %d tasks", len(fs.tasks))
	}
}
