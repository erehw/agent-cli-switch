package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decode(t *testing.T, b []byte) (string, map[string]string) {
	t.Helper()
	var f struct {
		APIKeyHelper string            `json:"apiKeyHelper"`
		Env          map[string]string `json:"env"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.APIKeyHelper, f.Env
}

func TestBuildHelperWritesFullEnvWithoutKey(t *testing.T) {
	spec := Spec{BaseURL: "http://127.0.0.1:1", Model: "m-main", Sonnet: "m-son", Extra: map[string]string{"API_TIMEOUT_MS": "60000"}}
	cmd, err := HelperCommand(`C:\Program Files\acsw\acsw.exe`, "gw1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(spec, ModeHelper, "", cmd)
	if err != nil {
		t.Fatal(err)
	}
	h, env := decode(t, b)
	if h != `"C:\Program Files\acsw\acsw.exe" claude key gw1` {
		t.Fatalf("apiKeyHelper = %q", h)
	}
	for _, n := range ManagedEnv {
		if _, ok := env[n]; !ok {
			t.Errorf("缺 %s", n)
		}
	}
	if env[EnvAuthToken] != "" || env[EnvAPIKey] != "" || env["CLAUDE_CODE_USE_BEDROCK"] != "" {
		t.Fatal("helper 模式 token/key/USE_* 应为空串")
	}
	if env[EnvBaseURL] != spec.BaseURL || env[EnvModel] != "m-main" || env[EnvSonnet] != "m-son" || env[EnvOpus] != "" || env["API_TIMEOUT_MS"] != "60000" {
		t.Fatalf("env 不对：%v", env)
	}
	if _, ok := env["CLAUDE_CONFIG_DIR"]; ok {
		t.Fatal("不应设置 CLAUDE_CONFIG_DIR")
	}
	if !strings.Contains(string(b), `\"C:\\Program Files`) {
		t.Fatalf("JSON 里路径应带转义引号：%s", b)
	}
}

func TestBuildPlainPutsKeyInAuthToken(t *testing.T) {
	b, err := Build(Spec{BaseURL: "http://x", Model: "m"}, ModePlain, "dummy-k", "")
	if err != nil {
		t.Fatal(err)
	}
	h, env := decode(t, b)
	if h != "" || env[EnvAuthToken] != "dummy-k" || env[EnvAPIKey] != "" {
		t.Fatalf("plain 不对：helper=%q env=%v", h, env)
	}
	if _, err := Build(Spec{BaseURL: "http://x", Model: "m"}, ModePlain, " ", ""); err == nil {
		t.Fatal("空 key 应报错")
	}
	if _, err := Build(Spec{BaseURL: "http://x", Model: "m"}, ModeHelper, "", ""); err == nil {
		t.Fatal("helper 缺命令应报错")
	}
}

func TestValidateExtraEnv(t *testing.T) {
	for _, bad := range []map[string]string{{"MY_API_KEY": "x"}, {"X_TOKEN": "x"}, {"secret_thing": "x"}, {"ANTHROPIC_MODEL": "x"}, {"1BAD": "x"}, {"A": "x\ny"}} {
		if err := ValidateExtraEnv(bad); err == nil {
			t.Errorf("%v 应被拒绝", bad)
		}
	}
	if err := ValidateExtraEnv(map[string]string{"API_TIMEOUT_MS": "1", "DISABLE_TELEMETRY": "1"}); err != nil {
		t.Fatal(err)
	}
}

func TestHelperCommandRejectsCmdSpecialChars(t *testing.T) {
	for _, p := range []string{`C:\a%b\acsw.exe`, `C:\a^b\acsw.exe`, `C:\a!b\acsw.exe`, ""} {
		if _, err := HelperCommand(p, "s"); err == nil {
			t.Errorf("%q 应被拒绝", p)
		}
	}
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{"": "", "helper": ModeHelper, "PLAIN": ModePlain, "file": ModePlain} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("%q → %q %v", in, got, err)
		}
	}
	if _, err := ParseMode("env"); err == nil {
		t.Fatal("env 应不认识")
	}
}

func TestInspect(t *testing.T) {
	cmd, _ := HelperCommand(`C:\x y\acsw.exe`, "s")
	b, _ := Build(Spec{BaseURL: "http://x", Model: "m"}, ModeHelper, "", cmd)
	in, err := Inspect(b)
	if err != nil || len(in.Missing) != 0 || !in.HelperQuoted || in.HelperExe != `C:\x y\acsw.exe` || in.HasPlainKey {
		t.Fatalf("%+v %v", in, err)
	}
	in, _ = Inspect([]byte(`{"apiKeyHelper":"C:\\x\\acsw.exe claude key s","env":{"ANTHROPIC_AUTH_TOKEN":"k"}}`))
	if in.HelperQuoted || in.HelperExe != `C:\x\acsw.exe` || !in.HasPlainKey || len(in.Missing) != len(ManagedEnv)-1 {
		t.Fatalf("%+v", in)
	}
}

func TestScanReportsNamesOnly(t *testing.T) {
	d := t.TempDir()
	user := filepath.Join(d, "settings.json")
	if err := os.WriteFile(user, []byte(`{"apiKeyHelper":"x","env":{"ANTHROPIC_AUTH_TOKEN":"dummy-user-token","ANTHROPIC_BETA_X":"1","CLAUDE_CODE_USE_BEDROCK":"1","CLAUDE_CODE_USE_POWERSHELL_TOOL":"1","OTHER":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(d, "bad.json")
	_ = os.WriteFile(bad, []byte(`{`), 0o600)
	fs := Scan([]string{user, bad, filepath.Join(d, "missing.json")})
	if len(fs) != 2 {
		t.Fatalf("%+v", fs)
	}
	f := fs[0]
	if strings.Join(f.Covered, ",") != "ANTHROPIC_AUTH_TOKEN,CLAUDE_CODE_USE_BEDROCK" || strings.Join(f.Uncovered, ",") != "ANTHROPIC_BETA_X" || !f.Helper {
		t.Fatalf("%+v", f)
	}
	if !f.Leaky(ModeHelper) || fs[1].Error == "" {
		t.Fatalf("%+v", fs)
	}
	b, _ := json.Marshal(fs)
	if strings.Contains(string(b), "dummy-user-token") {
		t.Fatal("扫描结果不应含值")
	}
	only := Finding{File: "x", Covered: []string{"ANTHROPIC_MODEL"}, Helper: true}
	if only.Leaky(ModeHelper) || !only.Leaky(ModePlain) {
		t.Fatal("helper 模式下别处的 apiKeyHelper 会被盖住；plain 模式才算漏")
	}
}

func TestConfigDir(t *testing.T) {
	if got := ConfigDir(func(string) string { return `D:\cfg` }); got != `D:\cfg` {
		t.Fatal(got)
	}
	if got := ConfigDir(func(string) string { return "" }); !strings.HasSuffix(got, ".claude") {
		t.Fatal(got)
	}
}
