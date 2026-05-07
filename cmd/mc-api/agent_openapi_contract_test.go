package main

import "testing"

func TestAgentScheduleOpenAPIContracts(t *testing.T) {
	t.Parallel()

	spec := loadOpenAPISpec(t)
	paths := specMap(t, spec, "paths")

	assertOperation(t, paths, "/api/v1/agent-schedules", "get", "listAgentSchedules", []string{"200", "401", "403"})
	assertOperation(t, paths, "/api/v1/agent-schedules/{id}/enable", "post", "enableAgentSchedule", []string{"200", "401", "403", "404"})
	assertOperation(t, paths, "/api/v1/agent-schedules/{id}/disable", "post", "disableAgentSchedule", []string{"200", "401", "403", "404"})
	assertResponseSchema(t, paths, "/api/v1/agent-schedules", "get", "200", "AgentSchedulesResponse")
	assertResponseSchema(t, paths, "/api/v1/agent-schedules/{id}/enable", "post", "200", "AgentScheduleResponse")
	assertResponseSchema(t, paths, "/api/v1/agent-schedules/{id}/disable", "post", "200", "AgentScheduleResponse")

	schemas := specMap(t, specMap(t, spec, "components"), "schemas")
	assertRequiredFields(t, schemas, "AgentSchedule", []string{"id", "agent_id", "enabled", "priority", "payload", "created_at", "updated_at"})
	assertRequiredFields(t, schemas, "AgentSchedulesResponse", []string{"schedules"})
	assertRequiredFields(t, schemas, "AgentScheduleResponse", []string{"schedule"})
}
