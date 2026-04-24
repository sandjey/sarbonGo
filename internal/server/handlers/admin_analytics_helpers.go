package handlers

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
