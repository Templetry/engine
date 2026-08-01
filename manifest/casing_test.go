package manifest

import "testing"

func TestCasings(t *testing.T) {
	cases := []struct{ value, casing, want string }{
		{"Mi Super App", "pascal", "MiSuperApp"},
		{"Mi Super App", "camel", "miSuperApp"},
		{"Mi Super App", "kebab", "mi-super-app"},
		{"Mi Super App", "snake", "mi_super_app"},
		{"Mi Super App", "flat", "misuperapp"},
		{"Mi Super App", "", "Mi Super App"},
		{"HTTPServer", "snake", "http_server"},
		{"template-app", "pascal", "TemplateApp"},
		{"my_app2", "kebab", "my-app2"},
	}
	for _, c := range cases {
		got, err := Casing(c.value, c.casing)
		if err != nil {
			t.Fatalf("Casing(%q, %q): %v", c.value, c.casing, err)
		}
		if got != c.want {
			t.Errorf("Casing(%q, %q) = %q, want %q", c.value, c.casing, got, c.want)
		}
	}
	if _, err := Casing("x", "shouty"); err == nil {
		t.Error("unknown casing should error")
	}
}

func TestExpand(t *testing.T) {
	vars := map[string]string{"project_name": "Demo Shop"}
	got, err := Expand("app-{project_name.kebab}-{project_name}", vars)
	if err != nil {
		t.Fatal(err)
	}
	if want := "app-demo-shop-Demo Shop"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := Expand("{missing}", vars); err == nil {
		t.Error("unknown variable should error")
	}
	if _, err := Expand("{project_name.shouty}", vars); err == nil {
		t.Error("unknown casing should error")
	}
}
