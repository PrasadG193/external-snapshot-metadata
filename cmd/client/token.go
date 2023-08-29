package main

import (
	"context"
)

type ServiceAccountAccess struct {
	token     string
	namespace string
}

func (sa ServiceAccountAccess) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": sa.token,
		"namespace":     sa.namespace,
	}, nil
}

// RequireTransportSecurity indicates whether the credentials requires transport security.
func (sa ServiceAccountAccess) RequireTransportSecurity() bool {
	return true
}
