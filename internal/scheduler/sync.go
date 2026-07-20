package scheduler

import (
	"context"
	"log"
	"time"

	"ordercore/internal/service"
)

type SyncScheduler struct {
	settings *service.SettingsService
	stopCh   chan struct{}
}

func NewSyncScheduler(settings *service.SettingsService) *SyncScheduler {
	return &SyncScheduler{
		settings: settings,
		stopCh:   make(chan struct{}),
	}
}

func (s *SyncScheduler) Start() {
	go s.loop()
}

func (s *SyncScheduler) Stop() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func (s *SyncScheduler) loop() {
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-timer.C:
			s.runOnce()
			timer.Reset(60 * time.Second)
		}
	}
}

func (s *SyncScheduler) runOnce() {
	if s.settings == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	log.Printf("[sync-scheduler] checking due sync jobs")
	s.settings.RunDueSyncJobs(ctx)
}
