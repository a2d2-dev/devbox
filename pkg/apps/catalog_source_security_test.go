package apps

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

// 静态公网解析器（测试注入）：恒返回一个公网 IP。
func publicResolver(ip string) hostResolver {
	return func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP(ip)}, nil
	}
}

func TestValidateDynamicCatalogURL_RejectsLocalAndPrivate(t *testing.T) {
	pub := publicResolver("93.184.216.34")
	for _, u := range []string{
		"https://127.0.0.1/x.git",
		"https://[::1]/x.git",
		"https://localhost/x.git",
		"https://169.254.169.254/x.git", // 云元数据（link-local）
		"https://10.0.0.1/x.git",
		"https://192.168.1.1/x.git",
		"https://172.16.0.1/x.git",
		"https://172.31.255.255/x.git",
		"https://0.0.0.0/x.git",
		"https://224.0.0.1/x.git",
		"https://foo.internal/x.git",
		"https://foo.localhost/x.git",
		"https://foo.local/x.git",
		"http://example.com/x.git", // 非 https
	} {
		err := validateDynamicCatalogURL(context.Background(), u, pub)
		require.Errorf(t, err, "%s 应被拒绝", u)
		require.NotContains(t, err.Error(), u, "错误不得回显 URL 全文")
	}
}

func TestValidateDynamicCatalogURL_AcceptsPublic(t *testing.T) {
	pub := publicResolver("140.82.121.4") // github
	require.NoError(t, validateDynamicCatalogURL(context.Background(),
		"https://github.com/1Panel-dev/appstore.git", pub))
}

// resolver 注入私网结果（DNS 引向内网）→ 拒绝。
func TestValidateDynamicCatalogURL_RejectsResolverPrivate(t *testing.T) {
	evil := func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.1.2.3")}, nil
	}
	require.Error(t, validateDynamicCatalogURL(context.Background(),
		"https://evil.example.com/x.git", evil))
}

// 解析返回混合（公网+私网）→ 任一私网即拒绝。
func TestValidateDynamicCatalogURL_RejectsMixedResolution(t *testing.T) {
	mixed := func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("140.82.121.4"), net.ParseIP("10.0.0.9")}, nil
	}
	require.Error(t, validateDynamicCatalogURL(context.Background(),
		"https://sneaky.example.com/x.git", mixed))
}

// 禁止 userinfo/query/fragment（token 须走配置，不进 URL）。
func TestValidateDynamicCatalogURL_RejectsUserInfoQuery(t *testing.T) {
	pub := publicResolver("140.82.121.4")
	for _, u := range []string{
		"https://user:pass@github.com/x.git",
		"https://github.com/x.git?token=leak",
		"https://github.com/x.git#frag",
	} {
		require.Error(t, validateDynamicCatalogURL(context.Background(), u, pub), u)
	}
}

// resolver 返回成功但零 IP → 拒绝（无法确认公网）。
func TestValidateDynamicCatalogURL_ZeroIPsRejected(t *testing.T) {
	zero := func(ctx context.Context, host string) ([]net.IP, error) { return nil, nil }
	require.Error(t, validateDynamicCatalogURL(context.Background(), "https://example.com/x.git", zero))
}
