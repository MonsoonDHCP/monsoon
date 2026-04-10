package auth

func HasRole(required string, actual string) bool {
	rank := map[string]int{
		DefaultRoleViewer:   1,
		DefaultRoleOperator: 2,
		DefaultRoleAdmin:    3,
	}
	return rank[sanitizeRole(actual)] >= rank[sanitizeRole(required)]
}
