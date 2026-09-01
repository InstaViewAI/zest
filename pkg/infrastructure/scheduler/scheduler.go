package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/robfig/cron/v3"

	"zest/pkg/application/escalation"
	"zest/pkg/infrastructure/config"
)

// jobTimeout bounds a single report run so a hung upstream call can't wedge the
// scheduler until the next tick.
const jobTimeout = 2 * time.Minute

// Scheduler runs the recurring stale-escalation digests that replace the
// Zapier Schedule triggers.
type Scheduler struct {
	cron      *cron.Cron
	service   *escalation.Service
	cfg       config.ReportConfig
	subdomain string
}

func New(service *escalation.Service, cfg config.ReportConfig, subdomain string) (*Scheduler, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, err
	}

	return &Scheduler{
		cron:      cron.New(cron.WithLocation(loc)),
		service:   service,
		cfg:       cfg,
		subdomain: subdomain,
	}, nil
}

// Start registers the enabled reports and begins running them in the
// background. A disabled report is simply not registered.
func (s *Scheduler) Start() error {
	if s.cfg.WeeklyEnabled {
		if err := s.register("Weekly Escalation Report", s.cfg.WeeklyCron); err != nil {
			return err
		}
	}

	if s.cfg.MonthlyEnabled {
		if err := s.register("Monthly Escalation Report", s.cfg.MonthlyCron); err != nil {
			return err
		}
	}

	if len(s.cron.Entries()) == 0 {
		log.Print("scheduler: no reports enabled")

		return nil
	}

	s.cron.Start()

	return nil
}

// Stop halts the scheduler and waits for a running job to finish.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

func (s *Scheduler) register(title, spec string) error {
	entryID, err := s.cron.AddFunc(spec, func() { s.run(title) })
	if err != nil {
		return err
	}

	log.Printf("scheduler: %q registered (%s, next run %s)",
		title, spec, s.cron.Entry(entryID).Next.Format(time.RFC3339))

	return nil
}

// run executes one report, logging rather than propagating failures so a bad
// run doesn't take down the scheduler.
func (s *Scheduler) run(title string) {
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()

	log.Printf("scheduler: running %q", title)

	res, err := s.service.PostStaleReport(ctx, title, s.cfg.StaleAfter, s.subdomain)
	if err != nil {
		log.Printf("scheduler: %q failed: %v", title, err)
		return
	}

	log.Printf("scheduler: %q posted, %d stale ticket(s)", title, res.Tickets)
}
