package main

import (
	"fmt"
	"io"

	"acsw/internal/core"
)

// printImportBase 打印 import-base 的预览或结果（只有键名，不打印任何值）。
func printImportBase(w io.Writer, r core.ImportBaseResult) {
	fmt.Fprintf(w, "从 %s（只读）导入到 %s\n", r.From, r.Config)
	act := map[string]string{"add": "新增", "replace": "覆盖"}
	for _, c := range r.Changes {
		fmt.Fprintf(w, "  %s  %s\n", act[c.Action], c.Key)
	}
	if len(r.Unchanged) > 0 {
		fmt.Fprintf(w, "  已相同，不动：%d 项\n", len(r.Unchanged))
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(w, "  跳过  %s：%s\n", s.Key, s.Reason)
	}
	for _, x := range r.Warnings {
		fmt.Fprintf(w, "注意：%s\n", x)
	}
	switch {
	case r.Written:
		if r.BackupFile != "" {
			fmt.Fprintf(w, "已写入。原 config.toml 备份在 %s\n", r.BackupFile)
		} else {
			fmt.Fprintln(w, "已写入（原来没有 config.toml）")
		}
	case r.DryRun && len(r.Changes) > 0:
		fmt.Fprintln(w, "这是预演，什么都没改。去掉 --dry-run 正式导入")
	}
}
