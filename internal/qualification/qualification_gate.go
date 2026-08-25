package qualification

import "fmt"

var requiredPromotionGates = map[string][]string{
	ModelArtifactSchemaV1: {"artifact-bytes-pinned", "artifact-license-review"},
	EngineSchemaV1:        {"engine-serving-contract", "engine-software-tested"},
	ModelRuntimeSchemaV1:  {"runtime-regression-disposition", "runtime-task-success"},
	ToolSchemaV1:          {"tool-permission-review", "tool-transport-contract"},
	ModeSchemaV1:          {"mode-composition", "mode-resource-fit"},
	ActivitySchemaV1:      {"activity-composition", "activity-scope-review"},
}

func validateQualifiedPromotionPacket(packet ProductPromotionPacket, problem func(string, ...any)) {
	if packet.Decision.QualificationStatus != QualificationStatusQualified {
		return
	}

	gates := make(map[string]ProductPromotionGate, len(packet.Decision.Gates))
	for _, gate := range packet.Decision.Gates {
		gates[gate.ID] = gate
		switch gate.Result {
		case "pass":
		case "not-applicable":
			if !qualifiedGateMayBeNotApplicable(packet, gate.ID) {
				problem("decision.gates gate %q cannot be not-applicable for this QUALIFIED target", gate.ID)
			}
		default:
			problem("decision.gates gate %q must pass for a QUALIFIED target, got %s", gate.ID, gate.Result)
		}
	}

	for _, gateID := range requiredPromotionGates[packet.Target.Schema] {
		if _, ok := gates[gateID]; !ok {
			problem("decision.gates must contain required QUALIFIED gate %q", gateID)
		}
	}
	for _, confound := range packet.Decision.Confounds {
		if confound.Disposition != "bounded" {
			problem("decision.confounds %q disposition %q is incompatible with QUALIFIED", confound.ID, confound.Disposition)
		}
	}
}

func qualifiedGateMayBeNotApplicable(packet ProductPromotionPacket, gateID string) bool {
	if gateID != "mode-resource-fit" {
		return false
	}
	mode, ok := packet.Candidate.Spec.(PromotionModeSpec)
	return ok && mode.Foreground != "local" && mode.WallModel.Result == "not-applicable"
}

func validateQualifiedProfileEvidence(envelope ProfileEnvelope, problem func(string, ...any)) {
	if envelope.QualificationStatus != QualificationStatusQualified {
		return
	}
	if len(envelope.Evidence) == 0 {
		problem("QUALIFIED profile requires accepted public evidence")
		return
	}

	for _, bucket := range envelope.Applicability.MachineBuckets {
		covered := false
		for _, evidence := range envelope.Evidence {
			if evidence.Scope.MachineBucket != nil && *evidence.Scope.MachineBucket == bucket {
				covered = true
				break
			}
		}
		if !covered {
			problem("QUALIFIED profile applicability machine bucket %s@%d has no exact evidence witness", bucket.ID, bucket.Revision)
		}
	}
	for _, harnessID := range envelope.Applicability.Harnesses {
		covered := false
		for _, evidence := range envelope.Evidence {
			for _, harness := range evidence.Scope.Harnesses {
				if harness.ID == harnessID {
					covered = true
					break
				}
			}
		}
		if !covered {
			problem("QUALIFIED profile applicability harness %q has no evidence witness", harnessID)
		}
	}
}

func validateQualifiedRuntime(profile ModelRuntimeProfile, problem func(string, ...any)) {
	if profile.QualificationStatus != QualificationStatusQualified {
		return
	}
	if profile.Spec.Performance.TaskSuccess.State != "measured" || !performanceHasMetric(profile.Spec.Performance.TaskSuccess, "first-attempt-task-success") {
		problem("QUALIFIED model runtime requires measured first-attempt-task-success")
	}
	if profile.Spec.Performance.Regressions.State != "measured" {
		problem("QUALIFIED model runtime requires measured regression disposition")
		return
	}
	for _, metric := range []string{"known-bad-tasks", "new-regressions", "retained-good-tasks"} {
		if !performanceHasMetric(profile.Spec.Performance.Regressions, metric) {
			problem("QUALIFIED model runtime regression disposition requires metric %q", metric)
		}
	}
}

func performanceHasMetric(axis PerformanceAxis, metric string) bool {
	for _, observation := range axis.Observations {
		if observation.Metric == metric {
			return true
		}
	}
	return false
}

func validateQualifiedToolHarnesses(profile ToolProfile, problem func(string, ...any)) {
	if profile.QualificationStatus != QualificationStatusQualified {
		return
	}
	for _, transport := range profile.Spec.Transports {
		if !evidenceHasHarness(profile.Evidence, transport.Harness, transport.IntegrationRevision) {
			problem("QUALIFIED tool transport %s@%s has no exact evidence witness", transport.Harness, transport.IntegrationRevision)
		}
	}
}

func validateQualifiedMode(profile ModeProfile, problem func(string, ...any)) {
	if profile.QualificationStatus != QualificationStatusQualified {
		return
	}
	if profile.Spec.Foreground == "local" && profile.Spec.WallModel.Result != "fit" {
		problem("QUALIFIED local mode requires a fit wall-model result")
	}
	if profile.Spec.WallModel.Result == "does-not-fit" || profile.Spec.WallModel.Result == "unmeasured" {
		problem("QUALIFIED mode cannot have wall-model result %q", profile.Spec.WallModel.Result)
	}
	for _, harness := range profile.Spec.Harnesses {
		if !evidenceHasHarness(profile.Evidence, harness.ID, harness.IntegrationRevision) {
			problem("QUALIFIED mode harness %s@%s has no exact evidence witness", harness.ID, harness.IntegrationRevision)
		}
	}
}

func evidenceHasHarness(evidence []ProfileEvidence, id, integrationRevision string) bool {
	for _, item := range evidence {
		for _, harness := range item.Scope.Harnesses {
			if harness.ID == id && harness.IntegrationRevision == integrationRevision {
				return true
			}
		}
	}
	return false
}

func validateDependencyDisposition(owner, dependency ProfileEnvelope) error {
	if owner.QualificationStatus != QualificationStatusQualified || owner.LifecycleStatus == LifecycleStatusRetired {
		return nil
	}
	if dependency.QualificationStatus != QualificationStatusQualified {
		return fmt.Errorf("requires dependency %s/%s@%d to be QUALIFIED, got %s", dependency.Schema, dependency.ID, dependency.Revision, dependency.QualificationStatus)
	}

	allowedLifecycle := false
	switch owner.LifecycleStatus {
	case LifecycleStatusExperimental:
		allowedLifecycle = dependency.LifecycleStatus == LifecycleStatusExperimental || dependency.LifecycleStatus == LifecycleStatusSupported
	case LifecycleStatusSupported:
		allowedLifecycle = dependency.LifecycleStatus == LifecycleStatusSupported
	case LifecycleStatusDeprecated:
		allowedLifecycle = dependency.LifecycleStatus == LifecycleStatusSupported || dependency.LifecycleStatus == LifecycleStatusDeprecated
	}
	if !allowedLifecycle {
		return fmt.Errorf("%s profile requires dependency %s/%s@%d lifecycle to be available in its closure, got %s", owner.LifecycleStatus, dependency.Schema, dependency.ID, dependency.Revision, dependency.LifecycleStatus)
	}
	return nil
}
