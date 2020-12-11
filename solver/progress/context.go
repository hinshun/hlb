package progress

import (
	"context"
)

type (
	managerKey struct{}
	jobKey     struct{}
)

func WithManager(ctx context.Context, m Manager) context.Context {
	return context.WithValue(ctx, managerKey{}, m)
}

func GetManager(ctx context.Context) Manager {
	m, _ := ctx.Value(managerKey{}).(Manager)
	return m
}

func WithJob(ctx context.Context, j Job) context.Context {
	return context.WithValue(ctx, jobKey{}, j)
}

func GetJob(ctx context.Context) Job {
	j, _ := ctx.Value(jobKey{}).(Job)
	return j
}
