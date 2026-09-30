package httpserver

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/activities"
	"github.com/crm/backend/internal/analytics"
	"github.com/crm/backend/internal/anzsco"
	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/automation"
	"github.com/crm/backend/internal/calls"
	"github.com/crm/backend/internal/communications"
	"github.com/crm/backend/internal/config"
	"github.com/crm/backend/internal/customers"
	"github.com/crm/backend/internal/customfields"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/deals"
	"github.com/crm/backend/internal/documents"
	"github.com/crm/backend/internal/email"
	"github.com/crm/backend/internal/forecasting"
	"github.com/crm/backend/internal/http/health"
	"github.com/crm/backend/internal/leads"
	"github.com/crm/backend/internal/leadsources"
	"github.com/crm/backend/internal/middleware"
	"github.com/crm/backend/internal/orgsettings"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/internal/pipelines"
	"github.com/crm/backend/internal/predictions"
	"github.com/crm/backend/internal/referrals"
	"github.com/crm/backend/internal/storage"
	"github.com/crm/backend/internal/systemactivity"
	"github.com/crm/backend/internal/targets"
	"github.com/crm/backend/internal/teams"
	"github.com/crm/backend/internal/timeline"
	"github.com/crm/backend/internal/users"
	"github.com/crm/backend/pkg/apperrors"
	"github.com/crm/backend/pkg/response"
)

type Dependencies struct {
	Config *config.Config
	Pool   *pgxpool.Pool
}

// NewRouter wires HTTP routes and returns the handler plus an automation worker
// that should be started with the process context.
func NewRouter(deps Dependencies) (http.Handler, *automation.Worker, *email.Worker) {
	mux := http.NewServeMux()

	healthChecker := health.NewChecker(deps.Pool, "0.1.0")
	healthHandler := health.NewHandler(healthChecker)

	hasher := auth.NewBcryptHasher()
	jwtSvc := auth.NewJWTService(deps.Config.JWTSecret)
	sessionRepo := auth.NewSessionRepository(deps.Pool)
	authService := auth.NewService(sessionRepo, jwtSvc, hasher)
	authHandler := auth.NewHandler(authService, deps.Config)
	authMW := auth.NewMiddleware(authService)

	auditRepo := audit.NewRepository(deps.Pool)
	auditService := audit.NewService(auditRepo)
	auditHandler := audit.NewHandler(auditService)

	store, err := storage.New(deps.Config.StorageDriver, deps.Config.StorageRoot)
	if err != nil {
		slog.Error("storage init failed", "error", err)
		store, _ = storage.NewLocal(deps.Config.StorageRoot)
	}

	timelineRepo := timeline.NewRepository(deps.Pool)
	timelineService := timeline.NewService(timelineRepo)
	timelineHandler := timeline.NewHandler(timelineService)

	automationEmitter := automation.NewEmitter(deps.Pool)
	autoRepo := automation.NewRepository(deps.Pool)
	autoExecutor := automation.NewExecutor(deps.Pool, autoRepo, auditService)
	autoWorker := automation.NewWorker(autoRepo, autoExecutor, automationEmitter)
	autoService := automation.NewService(autoRepo, autoExecutor, autoWorker, auditService)
	autoHandler := automation.NewHandler(autoService)

	docsRepo := documents.NewRepository(deps.Pool)
	docsService := documents.NewService(docsRepo, store, auditService, timelineService, automationEmitter)
	docsHandler := documents.NewHandler(docsService)

	usersRepo := users.NewRepository(deps.Pool)
	usersService := users.NewService(usersRepo, hasher, auditService, sessionRepo)
	usersHandler := users.NewHandler(usersService)

	teamsRepo := teams.NewRepository(deps.Pool)
	teamsService := teams.NewService(teamsRepo, auditService)
	teamsHandler := teams.NewHandler(teamsService)

	anzscoRepo := anzsco.NewRepository(deps.Pool)
	anzscoService := anzsco.NewService(anzscoRepo)
	anzscoHandler := anzsco.NewHandler(anzscoService)

	pipelinesRepo := pipelines.NewRepository(deps.Pool)
	pipelinesService := pipelines.NewService(pipelinesRepo, auditService, automationEmitter)
	pipelinesHandler := pipelines.NewHandler(pipelinesService)

	referralsRepo := referrals.NewRepository(deps.Pool)
	referralsService := referrals.NewService(referralsRepo, auditService)
	referralsHandler := referrals.NewHandler(referralsService)

	leadSourcesService := leadsources.NewService(deps.Pool, auditService)
	leadSourcesHandler := leadsources.NewHandler(leadSourcesService)

	customFieldsService := customfields.NewService(deps.Pool, auditService)
	customFieldsHandler := customfields.NewHandler(customFieldsService)

	orgSettingsService := orgsettings.NewService(deps.Pool, auditService)
	orgSettingsHandler := orgsettings.NewHandler(orgSettingsService)

	sysActivityService := systemactivity.NewService(deps.Pool)
	sysActivityHandler := systemactivity.NewHandler(sysActivityService)

	leadsRepo := leads.NewRepository(deps.Pool)
	leadsService := leads.NewService(leadsRepo, auditService, timelineService, automationEmitter, referralsService, customFieldsService, sysActivityService)
	leadsHandler := leads.NewHandler(leadsService)

	customersRepo := customers.NewRepository(deps.Pool)
	customersService := customers.NewService(customersRepo, leadsRepo, auditService, timelineService, automationEmitter, referralsService, customFieldsService, sysActivityService)
	customersHandler := customers.NewHandler(customersService)

	dealsRepo := deals.NewRepository(deps.Pool)
	dealsService := deals.NewService(dealsRepo, pipelinesService, auditService, automationEmitter, timelineService, sysActivityService)
	dealsHandler := deals.NewHandler(dealsService)

	activitiesRepo := activities.NewRepository(deps.Pool)
	activitiesService := activities.NewService(activitiesRepo, auditService, timelineService, automationEmitter, sysActivityService)
	activitiesHandler := activities.NewHandler(activitiesService)
	dealsService.SetFollowUps(activitiesService)

	callsService := calls.NewService(calls.NewRepository(deps.Pool), leadsRepo, customersRepo, timelineService)
	callsHandler := calls.NewHandler(callsService)

	analyticsRepo := analytics.NewRepository(deps.Pool)
	analyticsService := analytics.NewService(analyticsRepo)
	analyticsHandler := analytics.NewHandler(analyticsService)

	targetsRepo := targets.NewRepository(deps.Pool)
	targetsService := targets.NewService(targetsRepo, auditService)
	targetsHandler := targets.NewHandler(targetsService)

	forecastService := forecasting.NewService(deps.Pool)
	forecastHandler := forecasting.NewHandler(forecastService)

	predRepo := predictions.NewRepository(deps.Pool)
	predFeatures := predictions.NewCRMFeatureBuilder(deps.Pool)
	predService := predictions.NewService(predFeatures, predRepo)
	predHandler := predictions.NewHandler(predService)

	commsRepo := communications.NewRepository(deps.Pool)
	commsService := communications.NewService(
		commsRepo, timelineService, auditService,
		communications.NewMetaWhatsAppProvider(),
		communications.NewTwilioVoiceProvider(),
		deps.Config.PublicAPIURL,
	)
	commsHandler := communications.NewHandler(commsService)

	emailRepo := email.NewRepository(deps.Pool)
	emailService := email.NewService(emailRepo, deps.Config, auditService, timelineService)
	emailHandler := email.NewHandler(emailService)
	emailWorker := email.NewWorker(emailService)

	mux.HandleFunc("GET /api/health", healthHandler.Get)
	mux.HandleFunc("GET /api/v1/health", healthHandler.Get)

	mux.HandleFunc("POST /api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/v1/auth/logout", authHandler.Logout)
	mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.Refresh)
	mux.Handle("GET /api/v1/auth/me", authMW.Authenticate(http.HandlerFunc(authHandler.Me)))
	mux.Handle("PATCH /api/v1/auth/me", authMW.Authenticate(http.HandlerFunc(authHandler.UpdateMe)))
	mux.Handle("POST /api/v1/auth/change-password", authMW.Authenticate(http.HandlerFunc(authHandler.ChangePassword)))

	protect := func(perms []string, h http.HandlerFunc) http.Handler {
		var stack http.Handler = h
		if len(perms) > 0 {
			stack = auth.RequirePermission(perms...)(stack)
		}
		return authMW.Authenticate(stack)
	}

	// inScope 404s unless the {id} record is inside the caller's widest data scope
	// across perms. Handlers behind it load by id alone, so this is their ownership check.
	inScope := func(rec datascope.Record, perms []string, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			claims, _ := auth.ClaimsFromContext(r.Context())
			if err := datascope.RequireRecord(r.Context(), deps.Pool, claims, rec, r.PathValue("id"), perms...); err != nil {
				response.Fail(w, err)
				return
			}
			h(w, r)
		}
	}
	// customFieldRecord is inScope for /custom-fields/values/{entity}/{recordId}.
	customFieldRecord := func(edit bool, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var rec datascope.Record
			var perms []string
			switch r.PathValue("entity") {
			case "lead":
				rec, perms = datascope.Leads, []string{permissions.LeadsView, permissions.LeadsEdit}
			case "customer":
				rec, perms = datascope.Customers, []string{permissions.CustomersView, permissions.CustomersEdit}
			case "deal":
				rec, perms = datascope.Deals, []string{permissions.DealsView, permissions.DealsEdit}
			case "activity":
				rec, perms = datascope.Activities, []string{permissions.ActivitiesView, permissions.ActivitiesEdit}
			default:
				response.Fail(w, apperrors.NotFound("record not found"))
				return
			}
			// custom_fields:view is a catalog grant, so reads follow the record's own view scope.
			// custom_fields:manage is admin-only and lets Super Admin fill values without entity edit.
			if edit {
				perms = []string{perms[1], permissions.CustomFieldsManage}
			} else {
				perms = perms[:1]
			}
			claims, _ := auth.ClaimsFromContext(r.Context())
			if err := datascope.RequireRecord(r.Context(), deps.Pool, claims, rec, r.PathValue("recordId"), perms...); err != nil {
				response.Fail(w, err)
				return
			}
			h(w, r)
		}
	}

	mux.Handle("GET /api/v1/users", protect([]string{permissions.UsersView, permissions.UsersManage}, usersHandler.List))
	mux.Handle("POST /api/v1/users", protect([]string{permissions.UsersCreate, permissions.UsersManage}, usersHandler.Create))
	mux.Handle("GET /api/v1/users/{id}", protect([]string{permissions.UsersView, permissions.UsersManage}, usersHandler.Get))
	mux.Handle("PATCH /api/v1/users/{id}", protect([]string{permissions.UsersEdit, permissions.UsersManage, permissions.UsersAssign}, usersHandler.Update))
	mux.Handle("POST /api/v1/users/{id}/status", protect([]string{permissions.UsersDelete, permissions.UsersManage}, usersHandler.SetStatus))
	mux.Handle("POST /api/v1/users/bulk-status", protect([]string{permissions.UsersDelete, permissions.UsersManage}, usersHandler.BulkStatus))
	mux.Handle("GET /api/v1/roles", protect([]string{permissions.RolesView, permissions.RolesManage, permissions.UsersView}, usersHandler.ListRoles))

	mux.Handle("GET /api/v1/teams", protect([]string{permissions.TeamsView, permissions.TeamsManage}, teamsHandler.List))
	mux.Handle("POST /api/v1/teams", protect([]string{permissions.TeamsCreate, permissions.TeamsManage}, teamsHandler.Create))
	mux.Handle("GET /api/v1/teams/{id}", protect([]string{permissions.TeamsView, permissions.TeamsManage}, teamsHandler.Get))
	mux.Handle("PATCH /api/v1/teams/{id}", protect([]string{permissions.TeamsEdit, permissions.TeamsManage, permissions.TeamsAssign}, teamsHandler.Update))
	mux.Handle("POST /api/v1/teams/{id}/status", protect([]string{permissions.TeamsDelete, permissions.TeamsManage}, teamsHandler.SetStatus))

	mux.Handle("GET /api/v1/audit-logs", protect([]string{permissions.AuditView}, auditHandler.List))

	mux.Handle("GET /api/v1/anzsco", protect([]string{permissions.LeadsView, permissions.CustomersView}, anzscoHandler.Search))
	mux.Handle("GET /api/v1/pipelines", protect([]string{permissions.PipelinesView, permissions.LeadsView, permissions.DealsView, permissions.PipelinesManage}, pipelinesHandler.List))
	mux.Handle("GET /api/v1/pipelines/{id}", protect([]string{permissions.PipelinesView, permissions.DealsView, permissions.PipelinesManage}, pipelinesHandler.Get))
	mux.Handle("POST /api/v1/pipelines", protect([]string{permissions.PipelinesManage}, pipelinesHandler.Create))
	mux.Handle("PATCH /api/v1/pipelines/{id}", protect([]string{permissions.PipelinesManage}, pipelinesHandler.Update))
	mux.Handle("POST /api/v1/pipelines/{id}/stages", protect([]string{permissions.PipelinesManage}, pipelinesHandler.CreateStage))
	mux.Handle("POST /api/v1/pipelines/{id}/stages/reorder", protect([]string{permissions.PipelinesManage}, pipelinesHandler.ReorderStages))
	mux.Handle("PATCH /api/v1/pipeline-stages/{id}", protect([]string{permissions.PipelinesManage}, pipelinesHandler.UpdateStage))

	mux.Handle("GET /api/v1/leads", protect([]string{permissions.LeadsView}, leadsHandler.List))
	mux.Handle("POST /api/v1/leads", protect([]string{permissions.LeadsCreate}, leadsHandler.Create))
	leadView, leadEdit := []string{permissions.LeadsView}, []string{permissions.LeadsEdit}
	mux.Handle("GET /api/v1/leads/{id}", protect(leadView, inScope(datascope.Leads, leadView, leadsHandler.Get)))
	mux.Handle("PATCH /api/v1/leads/{id}", protect(leadEdit, inScope(datascope.Leads, leadEdit, leadsHandler.Update)))
	mux.Handle("POST /api/v1/leads/bulk-archive", protect([]string{permissions.LeadsDelete}, leadsHandler.BulkArchive))
	mux.Handle("POST /api/v1/leads/bulk-assign", protect([]string{permissions.LeadsAssign}, leadsHandler.BulkAssign))
	mux.Handle("POST /api/v1/leads/bulk-stage", protect([]string{permissions.LeadsEdit}, leadsHandler.BulkStage))
	mux.Handle("POST /api/v1/leads/check-duplicates", protect([]string{permissions.LeadsCreate, permissions.LeadsEdit}, leadsHandler.CheckDuplicates))
	mux.Handle("POST /api/v1/leads/{id}/qualify", protect(leadEdit, inScope(datascope.Leads, leadEdit, leadsHandler.Qualify)))
	mux.Handle("POST /api/v1/leads/{id}/convert", protect([]string{permissions.LeadsEdit, permissions.CustomersCreate}, inScope(datascope.Leads, leadEdit, customersHandler.ConvertLead)))

	customerView, customerEdit := []string{permissions.CustomersView}, []string{permissions.CustomersEdit}
	mux.Handle("GET /api/v1/customers", protect(customerView, customersHandler.List))
	mux.Handle("POST /api/v1/customers", protect([]string{permissions.CustomersCreate}, customersHandler.Create))
	mux.Handle("GET /api/v1/customers/{id}", protect(customerView, inScope(datascope.Customers, customerView, customersHandler.Get)))
	mux.Handle("GET /api/v1/customers/{id}/profile", protect(customerView, inScope(datascope.Customers, customerView, customersHandler.Get360)))
	mux.Handle("PATCH /api/v1/customers/{id}", protect(customerEdit, inScope(datascope.Customers, customerEdit, customersHandler.Update)))

	mux.Handle("GET /api/v1/deals", protect([]string{permissions.DealsView}, dealsHandler.List))
	mux.Handle("GET /api/v1/deals/board", protect([]string{permissions.DealsView}, dealsHandler.Board))
	mux.Handle("POST /api/v1/deals", protect([]string{permissions.DealsCreate}, dealsHandler.Create))
	dealView, dealEdit := []string{permissions.DealsView}, []string{permissions.DealsEdit}
	mux.Handle("GET /api/v1/deals/{id}", protect(dealView, inScope(datascope.Deals, dealView, dealsHandler.Get)))
	mux.Handle("PATCH /api/v1/deals/{id}", protect(dealEdit, inScope(datascope.Deals, dealEdit, dealsHandler.Update)))
	mux.Handle("POST /api/v1/deals/{id}/move", protect(dealEdit, inScope(datascope.Deals, dealEdit, dealsHandler.Move)))
	mux.Handle("POST /api/v1/deals/{id}/documents", protect(dealEdit, inScope(datascope.Deals, dealEdit, dealsHandler.AddDocument)))

	mux.Handle("GET /api/v1/lead-sources", protect([]string{permissions.LeadSourcesView, permissions.LeadsCreate, permissions.LeadsEdit}, leadSourcesHandler.List))
	mux.Handle("POST /api/v1/lead-sources", protect([]string{permissions.LeadSourcesManage}, leadSourcesHandler.Create))
	mux.Handle("PATCH /api/v1/lead-sources/{id}", protect([]string{permissions.LeadSourcesManage}, leadSourcesHandler.Update))
	mux.Handle("DELETE /api/v1/lead-sources/{id}", protect([]string{permissions.LeadSourcesManage}, leadSourcesHandler.Delete))

	mux.Handle("GET /api/v1/custom-fields", protect([]string{permissions.CustomFieldsView, permissions.LeadsView, permissions.CustomersView, permissions.DealsView, permissions.ActivitiesView}, customFieldsHandler.List))
	mux.Handle("POST /api/v1/custom-fields", protect([]string{permissions.CustomFieldsManage}, customFieldsHandler.Create))
	mux.Handle("GET /api/v1/custom-fields/values/{entity}/{recordId}", protect([]string{permissions.CustomFieldsView, permissions.LeadsView, permissions.CustomersView, permissions.DealsView, permissions.ActivitiesView}, customFieldRecord(false, customFieldsHandler.GetValues)))
	mux.Handle("PUT /api/v1/custom-fields/values/{entity}/{recordId}", protect([]string{permissions.LeadsEdit, permissions.CustomersEdit, permissions.DealsEdit, permissions.ActivitiesEdit, permissions.CustomFieldsManage}, customFieldRecord(true, customFieldsHandler.SetValues)))
	mux.Handle("GET /api/v1/custom-fields/{id}", protect([]string{permissions.CustomFieldsView, permissions.CustomFieldsManage}, customFieldsHandler.Get))
	mux.Handle("PATCH /api/v1/custom-fields/{id}", protect([]string{permissions.CustomFieldsManage}, customFieldsHandler.Update))
	mux.Handle("DELETE /api/v1/custom-fields/{id}", protect([]string{permissions.CustomFieldsManage}, customFieldsHandler.Delete))

	mux.Handle("GET /api/v1/admin/activity-types", protect([]string{permissions.ActivityTypesView, permissions.ActivityTypesManage}, activitiesHandler.AdminListTypes))
	mux.Handle("POST /api/v1/admin/activity-types", protect([]string{permissions.ActivityTypesManage}, activitiesHandler.CreateType))
	mux.Handle("PATCH /api/v1/admin/activity-types/{id}", protect([]string{permissions.ActivityTypesManage}, activitiesHandler.UpdateType))
	mux.Handle("DELETE /api/v1/admin/activity-types/{id}", protect([]string{permissions.ActivityTypesManage}, activitiesHandler.DeleteType))

	mux.Handle("GET /api/v1/system-activity", protect([]string{permissions.SystemView}, sysActivityHandler.List))
	mux.Handle("GET /api/v1/system-activity/{id}", protect([]string{permissions.SystemView}, sysActivityHandler.Get))

	mux.Handle("GET /api/v1/settings", protect([]string{permissions.SettingsView, permissions.SettingsManage}, orgSettingsHandler.List))
	mux.Handle("GET /api/v1/settings/defaults", protect([]string{
		permissions.SettingsView, permissions.LeadsCreate, permissions.CustomersCreate,
		permissions.DealsCreate, permissions.ActivitiesCreate,
	}, orgSettingsHandler.Defaults))
	mux.Handle("PUT /api/v1/settings", protect([]string{permissions.SettingsManage}, orgSettingsHandler.Update))

	mux.Handle("GET /api/v1/activity-types", protect([]string{permissions.ActivitiesView}, activitiesHandler.ListTypes))
	mux.Handle("GET /api/v1/activities", protect([]string{permissions.ActivitiesView}, activitiesHandler.List))
	mux.Handle("GET /api/v1/activities/calendar", protect([]string{permissions.ActivitiesView}, activitiesHandler.Calendar))
	mux.Handle("GET /api/v1/activities/follow-up", protect([]string{permissions.ActivitiesView}, activitiesHandler.FollowUp))
	mux.Handle("POST /api/v1/activities", protect([]string{permissions.ActivitiesCreate}, activitiesHandler.Create))
	activityView, activityEdit := []string{permissions.ActivitiesView}, []string{permissions.ActivitiesEdit}
	mux.Handle("GET /api/v1/activities/{id}", protect(activityView, inScope(datascope.Activities, activityView, activitiesHandler.Get)))
	mux.Handle("PATCH /api/v1/activities/{id}", protect(activityEdit, inScope(datascope.Activities, activityEdit, activitiesHandler.Update)))
	mux.Handle("GET /api/v1/me/timezone", protect(nil, activitiesHandler.GetTimezone))
	mux.Handle("PATCH /api/v1/me/timezone", protect(nil, activitiesHandler.SetTimezone))

	mux.Handle("GET /api/v1/documents", protect([]string{permissions.DocumentsView}, docsHandler.List))
	mux.Handle("POST /api/v1/documents/request", protect([]string{permissions.DocumentsCreate}, docsHandler.Request))
	mux.Handle("POST /api/v1/documents/upload", protect([]string{permissions.DocumentsCreate}, docsHandler.Upload))
	docView, docEdit := []string{permissions.DocumentsView}, []string{permissions.DocumentsEdit}
	docDelete, docCreate := []string{permissions.DocumentsDelete}, []string{permissions.DocumentsCreate}
	mux.Handle("GET /api/v1/documents/{id}", protect(docView, inScope(datascope.Documents, docView, docsHandler.Get)))
	mux.Handle("PATCH /api/v1/documents/{id}", protect(docEdit, inScope(datascope.Documents, docEdit, docsHandler.Update)))
	mux.Handle("DELETE /api/v1/documents/{id}", protect(docDelete, inScope(datascope.Documents, docDelete, docsHandler.Delete)))
	mux.Handle("GET /api/v1/documents/{id}/download", protect(docView, inScope(datascope.Documents, docView, docsHandler.Download)))
	mux.Handle("POST /api/v1/documents/{id}/upload", protect(docCreate, inScope(datascope.Documents, docCreate, docsHandler.Upload)))
	mux.Handle("POST /api/v1/documents/{id}/verify", protect(docEdit, inScope(datascope.Documents, docEdit, docsHandler.Verify)))
	mux.Handle("POST /api/v1/documents/{id}/reject", protect(docEdit, inScope(datascope.Documents, docEdit, docsHandler.Reject)))

	callsView := []string{permissions.LeadsView, permissions.CustomersView}
	mux.Handle("GET /api/v1/calls/contacts", protect(callsView, callsHandler.Contacts))
	mux.Handle("GET /api/v1/calls/context", protect(callsView, callsHandler.Context))
	mux.Handle("GET /api/v1/calls/history", protect(callsView, callsHandler.History))
	mux.Handle("GET /api/v1/calls/notes", protect(callsView, callsHandler.ListNotes))
	mux.Handle("POST /api/v1/calls/notes", protect([]string{permissions.ActivitiesCreate}, callsHandler.CreateNote))
	mux.Handle("PATCH /api/v1/calls/notes/{id}", protect([]string{permissions.ActivitiesEdit}, callsHandler.UpdateNote))
	mux.Handle("DELETE /api/v1/calls/notes/{id}", protect([]string{permissions.ActivitiesEdit}, callsHandler.DeleteNote))

	mux.Handle("GET /api/v1/timeline", protect([]string{permissions.CustomersView, permissions.LeadsView, permissions.DealsView, permissions.ActivitiesView}, timelineHandler.List))
	mux.Handle("GET /api/v1/timeline/types", protect([]string{permissions.CustomersView, permissions.LeadsView, permissions.DealsView}, timelineHandler.EventTypes))
	mux.Handle("GET /api/v1/timeline/{id}", protect([]string{permissions.CustomersView, permissions.LeadsView, permissions.DealsView}, timelineHandler.Get))
	mux.Handle("POST /api/v1/timeline/events", protect([]string{permissions.ActivitiesCreate, permissions.DocumentsCreate}, timelineHandler.WriteExternal))

	mux.Handle("GET /api/v1/analytics/funnel", protect([]string{permissions.AnalyticsView}, analyticsHandler.Funnel))
	mux.Handle("GET /api/v1/analytics/summary", protect([]string{permissions.AnalyticsView}, analyticsHandler.Summary))
	mux.Handle("GET /api/v1/analytics/leads", protect([]string{permissions.AnalyticsView}, analyticsHandler.Leads))
	mux.Handle("GET /api/v1/analytics/pipeline", protect([]string{permissions.AnalyticsView}, analyticsHandler.Pipeline))
	mux.Handle("GET /api/v1/analytics/activities", protect([]string{permissions.AnalyticsView}, analyticsHandler.Activities))
	mux.Handle("GET /api/v1/analytics/conversions", protect([]string{permissions.AnalyticsView}, analyticsHandler.Conversions))
	mux.Handle("GET /api/v1/analytics/teams", protect([]string{permissions.AnalyticsView}, analyticsHandler.Teams))
	mux.Handle("GET /api/v1/analytics/sources", protect([]string{permissions.AnalyticsView}, analyticsHandler.Sources))
	mux.Handle("GET /api/v1/analytics/organization", protect([]string{permissions.AnalyticsView}, analyticsHandler.Organization))

	mux.Handle("GET /api/v1/targets/metrics", protect([]string{permissions.TargetsView, permissions.TargetsManage}, targetsHandler.Metrics))
	mux.Handle("GET /api/v1/targets/progress", protect([]string{permissions.TargetsView, permissions.TargetsManage}, targetsHandler.ListProgress))
	mux.Handle("GET /api/v1/targets", protect([]string{permissions.TargetsView, permissions.TargetsManage}, targetsHandler.List))
	mux.Handle("POST /api/v1/targets", protect([]string{permissions.TargetsManage}, targetsHandler.Create))
	mux.Handle("GET /api/v1/targets/{id}", protect([]string{permissions.TargetsView, permissions.TargetsManage}, targetsHandler.Get))
	mux.Handle("GET /api/v1/targets/{id}/progress", protect([]string{permissions.TargetsView, permissions.TargetsManage}, targetsHandler.Progress))
	mux.Handle("PATCH /api/v1/targets/{id}", protect([]string{permissions.TargetsManage}, targetsHandler.Update))
	mux.Handle("DELETE /api/v1/targets/{id}", protect([]string{permissions.TargetsManage}, targetsHandler.Delete))

	mux.Handle("GET /api/v1/forecasts/methodology", protect([]string{permissions.ForecastsView}, forecastHandler.Methodology))
	mux.Handle("GET /api/v1/forecasts/pipeline", protect([]string{permissions.ForecastsView}, forecastHandler.PipelineForecast))

	mux.Handle("GET /api/v1/predictions/catalog", protect([]string{permissions.PredictionsView, permissions.PredictionsManage}, predHandler.Catalog))
	mux.Handle("GET /api/v1/predictions/workload", protect([]string{permissions.PredictionsView, permissions.PredictionsManage}, predHandler.Workload))
	mux.Handle("GET /api/v1/predictions/pipeline-risk", protect([]string{permissions.PredictionsView, permissions.PredictionsManage}, predHandler.PipelineRisk))
	// Scoped by the record's own view grant: predictions:view carries organization scope.
	predictionsAny := []string{permissions.PredictionsView, permissions.PredictionsManage}
	mux.Handle("POST /api/v1/predictions/leads/{id}/score", protect(predictionsAny, inScope(datascope.Leads, leadView, predHandler.ScoreLead)))
	mux.Handle("GET /api/v1/predictions/leads/{id}/score-history", protect(predictionsAny, inScope(datascope.Leads, leadView, predHandler.LeadScoreHistory)))
	mux.Handle("GET /api/v1/predictions/leads/{id}/insights", protect(predictionsAny, inScope(datascope.Leads, leadView, predHandler.LeadInsights)))
	mux.Handle("GET /api/v1/predictions/deals/{id}/insights", protect(predictionsAny, inScope(datascope.Deals, dealView, predHandler.DealInsights)))

	mux.Handle("GET /api/v1/automations/catalog", protect([]string{permissions.AutomationsView, permissions.AutomationsManage}, autoHandler.Catalog))
	mux.Handle("GET /api/v1/automations", protect([]string{permissions.AutomationsView, permissions.AutomationsManage}, autoHandler.List))
	mux.Handle("POST /api/v1/automations", protect([]string{permissions.AutomationsManage}, autoHandler.Create))
	mux.Handle("GET /api/v1/automations/runs", protect([]string{permissions.AutomationsView, permissions.AutomationsManage}, autoHandler.ListRuns))
	mux.Handle("GET /api/v1/automations/jobs", protect([]string{permissions.AutomationsView, permissions.AutomationsManage}, autoHandler.ListJobs))
	mux.Handle("POST /api/v1/automations/jobs/{id}/retry", protect([]string{permissions.AutomationsManage}, autoHandler.RetryJob))
	mux.Handle("GET /api/v1/automations/{id}", protect([]string{permissions.AutomationsView, permissions.AutomationsManage}, autoHandler.Get))
	mux.Handle("PATCH /api/v1/automations/{id}", protect([]string{permissions.AutomationsManage}, autoHandler.Update))
	mux.Handle("DELETE /api/v1/automations/{id}", protect([]string{permissions.AutomationsManage}, autoHandler.Delete))

	mux.Handle("GET /api/v1/communications/accounts", protect([]string{permissions.CommunicationsView, permissions.CommunicationsSend, permissions.CommunicationsManage}, commsHandler.ListAccounts))
	mux.Handle("POST /api/v1/communications/accounts", protect([]string{permissions.CommunicationsManage}, commsHandler.CreateAccount))
	mux.Handle("GET /api/v1/communications/whatsapp/messages", protect([]string{permissions.CommunicationsView, permissions.CommunicationsSend}, commsHandler.ListWhatsApp))
	mux.Handle("POST /api/v1/communications/whatsapp/send", protect([]string{permissions.CommunicationsSend}, commsHandler.SendWhatsApp))
	mux.Handle("GET /api/v1/communications/twilio/calls", protect([]string{permissions.CommunicationsView, permissions.CommunicationsSend}, commsHandler.ListCalls))
	mux.Handle("POST /api/v1/communications/twilio/calls", protect([]string{permissions.CommunicationsSend}, commsHandler.PlaceCall))

	mux.Handle("GET /api/v1/referrals/meta", protect([]string{permissions.ReferralsView, permissions.ReferralsManage, permissions.LeadsCreate, permissions.CustomersCreate}, referralsHandler.Meta))
	mux.Handle("GET /api/v1/referrals/summary", protect([]string{permissions.ReferralsView, permissions.ReferralsManage}, referralsHandler.Summary))
	mux.Handle("GET /api/v1/referrals/subject", protect([]string{permissions.ReferralsView, permissions.ReferralsManage, permissions.LeadsView, permissions.CustomersView}, referralsHandler.GetBySubject))
	mux.Handle("GET /api/v1/referrals", protect([]string{permissions.ReferralsView, permissions.ReferralsManage}, referralsHandler.List))
	mux.Handle("POST /api/v1/referrals", protect([]string{permissions.ReferralsCreate, permissions.ReferralsManage}, referralsHandler.Create))
	mux.Handle("GET /api/v1/referrals/{id}", protect([]string{permissions.ReferralsView, permissions.ReferralsManage}, referralsHandler.Get))
	mux.Handle("PATCH /api/v1/referrals/{id}", protect([]string{permissions.ReferralsEdit, permissions.ReferralsManage}, referralsHandler.Update))
	mux.Handle("GET /api/v1/referrers/{type}/{id}", protect([]string{permissions.ReferralsView, permissions.ReferralsManage}, referralsHandler.ReferrerProfile))
	mux.Handle("GET /api/v1/referral-partners", protect([]string{permissions.ReferralsView, permissions.ReferralsManage, permissions.LeadsCreate, permissions.CustomersCreate}, referralsHandler.ListPartners))
	mux.Handle("GET /api/v1/referrals/referrer-users", protect([]string{permissions.ReferralsView, permissions.ReferralsManage, permissions.LeadsCreate, permissions.CustomersCreate}, referralsHandler.ListReferrerUsers))
	mux.Handle("POST /api/v1/referral-partners", protect([]string{permissions.ReferralsCreate, permissions.ReferralsManage}, referralsHandler.CreatePartner))

	// Provider webhooks — public, authenticity validated per provider (no JWT).
	mux.HandleFunc("GET /api/v1/webhooks/meta/whatsapp", commsHandler.MetaWhatsAppVerify)
	mux.HandleFunc("POST /api/v1/webhooks/meta/whatsapp", commsHandler.MetaWhatsAppWebhook)
	mux.HandleFunc("POST /api/v1/webhooks/twilio/voice/status", commsHandler.TwilioVoiceStatus)

	mux.Handle("GET /api/v1/email/health", protect([]string{permissions.EmailView, permissions.EmailConfigure, permissions.EmailSend}, emailHandler.Health))
	mux.Handle("GET /api/v1/email/accounts", protect([]string{permissions.EmailView, permissions.EmailSend}, emailHandler.ListAccounts))
	mux.Handle("POST /api/v1/email/google/connect", protect([]string{permissions.EmailSend, permissions.EmailView}, emailHandler.Connect))
	mux.HandleFunc("GET /api/v1/email/google/callback", emailHandler.Callback)
	mux.Handle("POST /api/v1/email/accounts/{id}/disconnect", protect([]string{permissions.EmailSend, permissions.EmailView}, emailHandler.Disconnect))
	mux.Handle("POST /api/v1/email/sync/{accountId}", protect([]string{permissions.EmailView, permissions.EmailSend}, emailHandler.Sync))
	mux.Handle("GET /api/v1/email/threads", protect([]string{permissions.EmailView}, emailHandler.ListThreads))
	mux.Handle("GET /api/v1/email/threads/{id}", protect([]string{permissions.EmailView}, emailHandler.GetThread))
	mux.Handle("PATCH /api/v1/email/threads/{id}", protect([]string{permissions.EmailView, permissions.EmailSend}, emailHandler.PatchThread))
	mux.Handle("POST /api/v1/email/threads/{id}/associate", protect([]string{permissions.EmailManage, permissions.EmailSend, permissions.EmailView}, emailHandler.Associate))
	mux.Handle("POST /api/v1/email/messages", protect([]string{permissions.EmailSend}, emailHandler.Compose))
	mux.Handle("PATCH /api/v1/email/messages/{id}/draft", protect([]string{permissions.EmailSend}, emailHandler.PatchDraft))
	mux.Handle("POST /api/v1/email/messages/{id}/send", protect([]string{permissions.EmailSend}, emailHandler.Send))
	mux.Handle("POST /api/v1/email/preview", protect([]string{permissions.EmailSend, permissions.EmailView}, emailHandler.Preview))
	mux.Handle("GET /api/v1/email/templates", protect([]string{permissions.EmailView, permissions.EmailManage}, emailHandler.ListTemplates))
	mux.Handle("POST /api/v1/email/templates", protect([]string{permissions.EmailManage}, emailHandler.CreateTemplate))
	mux.Handle("PATCH /api/v1/email/templates/{id}", protect([]string{permissions.EmailManage}, emailHandler.UpdateTemplate))
	mux.Handle("DELETE /api/v1/email/templates/{id}", protect([]string{permissions.EmailManage}, emailHandler.DeleteTemplate))
	mux.HandleFunc("POST /api/v1/webhooks/google/pubsub", emailHandler.PubSub)

	return middleware.Chain(
		mux,
		middleware.Recover,
		middleware.RequestID,
		middleware.Logger,
		middleware.CORS(deps.Config.CORSOrigins),
	), autoWorker, emailWorker
}
