package v1alpha1

const (
	ConditionTypeInitializing string = "Initializing"
	ConditionTypeInProgress   string = "InProgress"
	ConditionTypeFailed       string = "Failed"
	ConditionTypeReady        string = "Ready"

	ConditionReasonReconciling = "Reconciling"
	ConditionReasonFailed      = "Failed"

	ConditionReasonDeploymentInProgress = "DeploymentInProgress"
	ConditionReasonDeploymentDone       = "DeploymentDone"
	ConditionReasonDeploymentFailed     = "DeploymentFailed"
)

const (
	DeploymentInProgressPhase = "DeploymentInProgress"
	DeploymentDonePhase       = "DeploymentDone"
	DeploymentFailedPhase     = "DeploymentFailed"
)

func StatusPhaseToReason(phase string) string {
	switch phase {
	case DeploymentInProgressPhase:
		return ConditionReasonDeploymentInProgress
	case DeploymentDonePhase:
		return ConditionReasonDeploymentDone
	case DeploymentFailedPhase:
		return ConditionReasonDeploymentFailed
	}
	return phase
}
