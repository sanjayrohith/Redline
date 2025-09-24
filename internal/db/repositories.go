package db

// Repositories aggregates every typed repository so the App constructs
// them once from a single pool.
type Repositories struct {
	Users            *UserRepository
	Orgs             *OrgRepository
	APIKeys          *APIKeyRepository
	Models           *ModelRepository
	Deployments      *DeploymentRepository
	InferenceRuns    *InferenceRunRepository
	TelemetrySamples *TelemetrySampleRepository
}

// NewRepositories constructs every repository bound to pool.
func NewRepositories(pool *Pool) *Repositories {
	return &Repositories{
		Users:            NewUserRepository(pool),
		Orgs:             NewOrgRepository(pool),
		APIKeys:          NewAPIKeyRepository(pool),
		Models:           NewModelRepository(pool),
		Deployments:      NewDeploymentRepository(pool),
		InferenceRuns:    NewInferenceRunRepository(pool),
		TelemetrySamples: NewTelemetrySampleRepository(pool),
	}
}
