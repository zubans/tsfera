package platform

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// PeriodicJob описывает фоновую задачу модуля, запускаемую единым планировщиком.
type PeriodicJob struct {
	Name      string
	Interval  time.Duration
	Immediate bool
	Run       func(ctx context.Context) error
}

// WorkerGroup управляет жизненным циклом периодических задач и корректным завершением по ctx.
type WorkerGroup struct {
	logger *slog.Logger
	jobs   []PeriodicJob
	wg     sync.WaitGroup
}

func NewWorkerGroup(logger *slog.Logger) *WorkerGroup {
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkerGroup{logger: logger}
}

func (g *WorkerGroup) Add(jobs ...PeriodicJob) {
	for _, j := range jobs {
		if j.Run == nil || j.Interval <= 0 {
			continue
		}
		g.jobs = append(g.jobs, j)
	}
}

// Start запускает все зарегистрированные задачи в отдельных горутинах до отмены ctx.
func (g *WorkerGroup) Start(ctx context.Context) {
	for _, job := range g.jobs {
		j := job
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			RunPeriodic(ctx, j, g.logger)
		}()
	}
}

// Wait ожидает завершения всех текущих проходов после отмены ctx.
func (g *WorkerGroup) Wait() {
	g.wg.Wait()
}

// RunPeriodic выполняет задачу по интервалу с перехватом паник и защитой от перекрытий.
func RunPeriodic(ctx context.Context, job PeriodicJob, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	runSafe := func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("worker panicked", "worker", job.Name, "panic", r)
			}
		}()
		if err := job.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("worker run failed", "worker", job.Name, "error", err)
		}
	}

	if job.Immediate {
		runSafe()
	}

	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runSafe()
		}
	}
}
