package pkg

import (
	"context"
	"main/pkg/data"
	databasePkg "main/pkg/database"
	"main/pkg/fs"
	"main/pkg/logger"
	mutes "main/pkg/mutes"
	"main/pkg/report"
	reportersPkg "main/pkg/reporters"
	"main/pkg/reporters/discord"
	"main/pkg/reporters/pagerduty"
	"main/pkg/reporters/telegram"
	"main/pkg/state"
	"main/pkg/tracing"
	"main/pkg/types"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

type App struct {
	Tracer           trace.Tracer
	Logger           *zerolog.Logger
	Config           *types.Config
	ReportGenerator  *report.Generator
	StateGenerator   *state.Generator
	ReportDispatcher *report.Dispatcher
	Database         databasePkg.Database
	StopChannel      chan bool
}

func NewApp(configPath string, filesystem fs.FS, version string) *App {
	config, err := GetConfig(filesystem, configPath)
	if err != nil {
		logger.GetDefaultLogger().Panic().Err(err).Msg("Could not load config")
	}

	if err = config.Validate(); err != nil {
		logger.GetDefaultLogger().Panic().Err(err).Msg("Provided config is invalid!")
	}

	if warnings := config.DisplayWarnings(); len(warnings) > 0 {
		config.LogWarnings(logger.GetDefaultLogger(), warnings)
	} else {
		logger.GetDefaultLogger().Info().Msg("Provided config is valid.")
	}

	tracer := tracing.InitTracer(config.TracingConfig, version)
	log := logger.GetLogger(config.LogConfig)

	database := databasePkg.NewSqliteDatabase(log, config.DatabaseConfig)

	mutesManager := mutes.NewMutesManager(log, database)
	stateGenerator := state.NewStateGenerator(log, tracer, config.Chains)
	dataManager := data.NewManager(log, config.Chains, tracer)

	generator := report.NewReportNewGenerator(log, config.Chains, database, tracer)

	timeZone, _ := time.LoadLocation(config.Timezone)

	reporters := []reportersPkg.Reporter{
		pagerduty.NewPagerDutyReporter(config.PagerDutyConfig, log, tracer),
		telegram.NewTelegramReporter(
			config.TelegramConfig,
			mutesManager,
			stateGenerator,
			dataManager,
			log,
			version,
			timeZone,
			tracer,
		),
		discord.NewReporter(
			config,
			version,
			log,
			mutesManager,
			dataManager,
			stateGenerator,
			timeZone,
			tracer,
		),
	}

	reportDispatcher := report.NewDispatcher(log, mutesManager, reporters, tracer)

	return &App{
		Tracer:           tracer,
		Logger:           log,
		Config:           config,
		ReportGenerator:  generator,
		StateGenerator:   stateGenerator,
		ReportDispatcher: reportDispatcher,
		Database:         database,
		StopChannel:      make(chan bool),
	}
}

func (a *App) Start() {
	a.Database.Init()
	a.Database.Migrate()

	if err := a.ReportDispatcher.Init(); err != nil {
		a.Logger.Panic().Err(err).Msg("Error initializing reporters")
	}

	c := cron.New()
	if _, err := c.AddFunc(a.Config.Interval, a.Report); err != nil {
		a.Logger.Panic().Err(err).Msg("Error processing cron pattern")
	}
	c.Start()
	a.Logger.Info().Str("interval", a.Config.Interval).Msg("Scheduled proposals reporting")

	<-a.StopChannel
	a.Logger.Info().Msg("Shutting down...")
	c.Stop()
}

func (a *App) Stop() {
	a.StopChannel <- true
}

func (a *App) Report() {
	ctx, span := a.Tracer.Start(context.Background(), "report")
	defer span.End()

	generatedReport := a.ReportGenerator.GenerateReport(ctx)
	a.ReportDispatcher.SendReport(generatedReport, ctx)
}
