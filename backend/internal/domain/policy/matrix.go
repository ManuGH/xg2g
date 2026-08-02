package policy

// ConflictRule defines the static policy outcome for a specific (Existing, Incoming) consumer pair.
type ConflictRule struct {
	Preemptible bool
	ReasonCode  ReasonCode
}

// ConflictMatrix maps [ExistingConsumer][IncomingConsumer] to a static ConflictRule.
type ConflictMatrix map[ConsumerType]map[ConsumerType]ConflictRule

// DefaultConflictMatrix is the authoritative, static 36-entry conflict rule matrix (ADR-031).
// Index order: DefaultConflictMatrix[existingConsumer][incomingConsumer]
var DefaultConflictMatrix = ConflictMatrix{
	// 1. Existing: SCHEDULED_RECORDING (Sacrosanct - Unconditionally Protects Recording)
	ConsumerScheduledRecording: {
		ConsumerScheduledRecording: {Preemptible: false, ReasonCode: ReasonPolicyRejectedProtectedActivity},
		ConsumerManualRecording:    {Preemptible: false, ReasonCode: ReasonPolicyRejectedProtectedActivity},
		ConsumerLiveTV:             {Preemptible: false, ReasonCode: ReasonPolicyRejectedProtectedActivity},
		ConsumerRetroDVR:           {Preemptible: false, ReasonCode: ReasonPolicyRejectedProtectedActivity},
		ConsumerChannelScan:        {Preemptible: false, ReasonCode: ReasonPolicyRejectedProtectedActivity},
		ConsumerBackgroundTransfer: {Preemptible: false, ReasonCode: ReasonPolicyRejectedProtectedActivity},
	},

	// 2. Existing: MANUAL_RECORDING
	ConsumerManualRecording: {
		ConsumerScheduledRecording: {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerManualRecording:    {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
		ConsumerLiveTV:             {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
		ConsumerRetroDVR:           {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
		ConsumerChannelScan:        {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
		ConsumerBackgroundTransfer: {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
	},

	// 3. Existing: LIVE_TV
	ConsumerLiveTV: {
		ConsumerScheduledRecording: {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerManualRecording:    {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerLiveTV:             {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
		ConsumerRetroDVR:           {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
		ConsumerChannelScan:        {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
		ConsumerBackgroundTransfer: {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
	},

	// 4. Existing: RETRO_DVR
	ConsumerRetroDVR: {
		ConsumerScheduledRecording: {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerManualRecording:    {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerLiveTV:             {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerRetroDVR:           {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
		ConsumerChannelScan:        {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
		ConsumerBackgroundTransfer: {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
	},

	// 5. Existing: CHANNEL_SCAN
	ConsumerChannelScan: {
		ConsumerScheduledRecording: {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerManualRecording:    {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerLiveTV:             {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerRetroDVR:           {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerChannelScan:        {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
		ConsumerBackgroundTransfer: {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
	},

	// 6. Existing: BACKGROUND_TRANSFER
	ConsumerBackgroundTransfer: {
		ConsumerScheduledRecording: {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerManualRecording:    {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerLiveTV:             {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerRetroDVR:           {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerChannelScan:        {Preemptible: true, ReasonCode: ReasonPolicyPreemptionRequired},
		ConsumerBackgroundTransfer: {Preemptible: false, ReasonCode: ReasonPolicyRejectedEqualOrLowerPriority},
	},
}
