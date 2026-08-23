package users

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestValidation(t *testing.T) {
	require.ErrorIs(t, ValidateUsername("x"), ErrInvalidUsername)
	require.NoError(t, ValidateUsername("dev.user-1"))
	require.ErrorIs(t, ValidatePassword("alllowercase"), ErrWeakPassword)
	require.NoError(t, ValidatePassword("Devbox-2026"))
	require.ErrorIs(t, ValidatePassword(" Devbox-2026"), ErrPasswordWhitespace)
	require.ErrorIs(t, ValidatePassword("Devbox-2026\t"), ErrPasswordWhitespace)
}

func TestUsersAuthenticateAndProtectLastAdmin(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, CreateUser{Username: "admin", DisplayName: "Admin", Password: "Devbox-2026", Role: RoleAdmin, Enabled: true})
	require.NoError(t, err)
	_, ok := s.Authenticate(ctx, "admin", "Devbox-2026")
	require.True(t, ok)
	_, ok = s.Authenticate(ctx, "admin", "wrong")
	require.False(t, ok)
	require.ErrorIs(t, s.DeleteUser(ctx, admin.ID), ErrLastAdmin)
	user, err := s.CreateUser(ctx, CreateUser{Username: "developer", Password: "Developer-2026", Role: RoleUser, Enabled: true})
	require.NoError(t, err)
	_, err = s.CreateUser(ctx, CreateUser{Username: "DEVELOPER", Password: "Different-2026", Role: RoleUser, Enabled: true})
	require.ErrorIs(t, err, ErrConflict)
	role := RoleAdmin
	_, err = s.UpdateUser(ctx, user.ID, UpdateUser{Role: &role})
	require.NoError(t, err)
	require.NoError(t, s.DeleteUser(ctx, admin.ID))
}

func TestFirstUserIsAlwaysEnabledAdministrator(t *testing.T) {
	s := testStore(t)
	u, err := s.CreateUser(context.Background(), CreateUser{
		Username: "first-user", Password: "First-user-2026", Role: RoleUser, Enabled: false,
	})
	require.NoError(t, err)
	require.Equal(t, RoleAdmin, u.Role)
	require.True(t, u.Enabled)

	users, err := s.ListUsers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, RoleAdmin, users[0].Role)
	require.True(t, users[0].Enabled)
}

func TestGroupAndDirectRootAuthorization(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, CreateUser{Username: "developer", Password: "Developer-2026", Role: RoleUser, Enabled: true})
	require.NoError(t, err)
	direct, err := s.CreateRoot(ctx, "Models", "/data/models")
	require.NoError(t, err)
	shared, err := s.CreateRoot(ctx, "Datasets", "/data/datasets")
	require.NoError(t, err)
	require.NoError(t, s.SetUserRoots(ctx, u.ID, []string{direct.ID}))
	g, err := s.CreateGroup(ctx, "ML Team", "", []string{u.ID})
	require.NoError(t, err)
	groups, err := s.ListGroups(ctx, "ml")
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, []string{u.ID}, groups[0].MemberIDs)
	require.NoError(t, s.SetGroupRoots(ctx, g.ID, []string{shared.ID}))
	paths, err := s.AllowedPaths(ctx, u.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/data/models", "/data/datasets"}, paths)
	require.NoError(t, s.DeleteRoot(ctx, direct.ID))
	ids, err := s.UserRootIDs(ctx, u.ID)
	require.NoError(t, err)
	require.Empty(t, ids)
	require.True(t, errors.Is(s.DeleteRoot(ctx, "missing"), ErrNotFound))
}

func TestUserAndRootGrantsRollbackTogether(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, err := s.CreateUser(ctx, CreateUser{Username: "admin", Password: "Admin-pass-2026", Role: RoleAdmin, Enabled: true})
	require.NoError(t, err)

	_, err = s.CreateUserWithRoots(ctx, CreateUser{
		Username: "developer", DisplayName: "Developer", Password: "Developer-2026", Role: RoleUser, Enabled: true,
	}, []string{"missing-root"})
	require.Error(t, err)
	users, err := s.ListUsers(ctx, "developer")
	require.NoError(t, err)
	require.Empty(t, users)

	root, err := s.CreateRoot(ctx, "Workspace", "/data/workspace")
	require.NoError(t, err)
	developer, err := s.CreateUserWithRoots(ctx, CreateUser{
		Username: "developer", DisplayName: "Developer", Password: "Developer-2026", Role: RoleUser, Enabled: true,
	}, []string{root.ID})
	require.NoError(t, err)
	changed := "Changed"
	_, err = s.UpdateUserWithRoots(ctx, developer.ID, UpdateUser{DisplayName: &changed}, []string{"missing-root"})
	require.Error(t, err)
	users, err = s.ListUsers(ctx, "developer")
	require.NoError(t, err)
	require.Equal(t, "Developer", users[0].DisplayName)
	rootIDs, err := s.UserRootIDs(ctx, developer.ID)
	require.NoError(t, err)
	require.Equal(t, []string{root.ID}, rootIDs)
}
