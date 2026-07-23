package v3

func intPtr(i int) *int {
	return &i
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}
