package policy

// ConflictRule defines the static policy outcome for a specific (Existing, Incoming) consumer pair.
type ConflictRule struct {
	Decision   PreemptionDecision
	ReasonCode ReasonCode
	LossClass  LossClass
}

// conflictMatrix maps [ExistingConsumer][IncomingConsumer] to a static ConflictRule.
type conflictMatrix map[ConsumerType]map[ConsumerType]ConflictRule

// defaultConflictMatrix is the unexported, immutable authoritative 36-entry conflict rule matrix (ADR-031).
// Index order: defaultConflictMatrix[existingConsumer][incomingConsumer]
var defaultConflictMatrix = conflictMatrix{
	// 1. Existing: SCHEDULED_RECORDING (Sacrosanct - Unconditionally Protects Recording)
	ConsumerScheduledRecording: {
		ConsumerScheduledRecording: {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedProtectedActivity, LossClass: LossScheduled},
		ConsumerManualRecording:    {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedProtectedActivity, LossClass: LossScheduled},
		ConsumerLiveTV:             {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedProtectedActivity, LossClass: LossScheduled},
		ConsumerRetroDVR:           {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedProtectedActivity, LossClass: LossScheduled},
		ConsumerChannelScan:        {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedProtectedActivity, LossClass: LossScheduled},
		ConsumerBackgroundTransfer: {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedProtectedActivity, LossClass: LossScheduled},
	},

	// 2. Existing: MANUAL_RECORDING
	ConsumerManualRecording: {
		ConsumerScheduledRecording: {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossManual},
		ConsumerManualRecording:    {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossManual},
		ConsumerLiveTV:             {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossManual},
		ConsumerRetroDVR:           {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossManual},
		ConsumerChannelScan:        {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossManual},
		ConsumerBackgroundTransfer: {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossManual},
	},

	// 3. Existing: LIVE_TV
	ConsumerLiveTV: {
		ConsumerScheduledRecording: {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossLiveTV},
		ConsumerManualRecording:    {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossLiveTV},
		ConsumerLiveTV:             {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossLiveTV},
		ConsumerRetroDVR:           {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossLiveTV},
		ConsumerChannelScan:        {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossLiveTV},
		ConsumerBackgroundTransfer: {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossLiveTV},
	},

	// 4. Existing: RETRO_DVR
	ConsumerRetroDVR: {
		ConsumerScheduledRecording: {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossRetroDVR},
		ConsumerManualRecording:    {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossRetroDVR},
		ConsumerLiveTV:             {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossRetroDVR},
		ConsumerRetroDVR:           {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossRetroDVR},
		ConsumerChannelScan:        {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossRetroDVR},
		ConsumerBackgroundTransfer: {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossRetroDVR},
	},

	// 5. Existing: CHANNEL_SCAN
	ConsumerChannelScan: {
		ConsumerScheduledRecording: {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossScan},
		ConsumerManualRecording:    {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossScan},
		ConsumerLiveTV:             {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossScan},
		ConsumerRetroDVR:           {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossScan},
		ConsumerChannelScan:        {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossScan},
		ConsumerBackgroundTransfer: {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossScan},
	},

	// 6. Existing: BACKGROUND_TRANSFER
	ConsumerBackgroundTransfer: {
		ConsumerScheduledRecording: {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossBackground},
		ConsumerManualRecording:    {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossBackground},
		ConsumerLiveTV:             {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossBackground},
		ConsumerRetroDVR:           {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossBackground},
		ConsumerChannelScan:        {Decision: DecisionPreemptionRequired, ReasonCode: ReasonPolicyPreemptionRequired, LossClass: LossBackground},
		ConsumerBackgroundTransfer: {Decision: DecisionReject, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority, LossClass: LossBackground},
	},
}

// ConflictRuleFor safely returns the immutable ConflictRule for an (Existing, Incoming) consumer pair.
// Callers cannot mutate the global internal matrix.
func ConflictRuleFor(existing, incoming ConsumerType) (ConflictRule, bool) {
	row, exists := defaultConflictMatrix[existing]
	if !exists {
		return ConflictRule{}, false
	}
	rule, exists := row[incoming]
	if !exists {
		return ConflictRule{}, false
	}
	return rule, true
}
