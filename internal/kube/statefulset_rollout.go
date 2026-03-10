package kube

import appsv1 "k8s.io/api/apps/v1"

type statefulSetRolloutExpectation struct {
	DesiredReplicas          int32
	ExpectedUpdatedReplicas  int32
	RequireRevisionAlignment bool
}

func statefulSetRolloutExpectationFor(sts *appsv1.StatefulSet) statefulSetRolloutExpectation {
	desired := int32(1)
	if sts.Spec.Replicas != nil {
		desired = *sts.Spec.Replicas
	}
	if desired < 0 {
		desired = 0
	}

	switch normalizedStatefulSetStrategyType(sts) {
	case appsv1.OnDeleteStatefulSetStrategyType:
		// OnDelete does not update pods automatically, so updated replica count must not block completion.
		return statefulSetRolloutExpectation{
			DesiredReplicas:          desired,
			ExpectedUpdatedReplicas:  0,
			RequireRevisionAlignment: false,
		}
	default:
		partition := statefulSetPartition(sts)
		expectedUpdated := desired
		if partition >= desired {
			expectedUpdated = 0
		} else if partition > 0 {
			expectedUpdated = desired - partition
		}

		return statefulSetRolloutExpectation{
			DesiredReplicas:          desired,
			ExpectedUpdatedReplicas:  expectedUpdated,
			RequireRevisionAlignment: desired > 0 && partition == 0,
		}
	}
}

func isStatefulSetRolloutComplete(sts *appsv1.StatefulSet) bool {
	expectation := statefulSetRolloutExpectationFor(sts)

	done := sts.Status.ObservedGeneration >= sts.Generation &&
		sts.Status.UpdatedReplicas >= expectation.ExpectedUpdatedReplicas &&
		sts.Status.ReadyReplicas == expectation.DesiredReplicas

	if done && expectation.RequireRevisionAlignment {
		done = sts.Status.CurrentRevision == sts.Status.UpdateRevision
	}

	return done
}

func normalizedStatefulSetStrategyType(sts *appsv1.StatefulSet) appsv1.StatefulSetUpdateStrategyType {
	strategy := sts.Spec.UpdateStrategy.Type
	if strategy == "" {
		return appsv1.RollingUpdateStatefulSetStrategyType
	}
	return strategy
}

func statefulSetPartition(sts *appsv1.StatefulSet) int32 {
	if normalizedStatefulSetStrategyType(sts) != appsv1.RollingUpdateStatefulSetStrategyType {
		return 0
	}

	rolling := sts.Spec.UpdateStrategy.RollingUpdate
	if rolling == nil || rolling.Partition == nil {
		return 0
	}

	if *rolling.Partition < 0 {
		return 0
	}

	return *rolling.Partition
}
