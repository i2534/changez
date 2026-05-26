// Package dbutil 提供从 map[string]any（DB 行映射）安全解引用指针字段的小工具。
//
// 背景：db 包的查询方法把 nullable 列扫描成 *int64 / *string，再放到 map[string]any 里返回。
// 直接 .(*int64) 或 .(*string) 后做 nil 比较是安全的；但若调用方先把 interface 取出来再
// 与 nil 比较（典型 typed-nil 陷阱），则永远不为 nil。这里统一封装，让调用点更短、更不易出错。
package dbutil

// AsInt64Ptr 把 map[string]any 里的 *int64 字段解引用为 int64。
// 若值缺失或为 nil（包括 typed-nil），返回 (0, false)。
func AsInt64Ptr(v any) (int64, bool) {
	p, _ := v.(*int64)
	if p == nil {
		return 0, false
	}
	return *p, true
}

// AsStringPtr 把 map[string]any 里的 *string 字段解引用为 string。
// 若值缺失或为 nil（包括 typed-nil），返回 ("", false)。
func AsStringPtr(v any) (string, bool) {
	p, _ := v.(*string)
	if p == nil {
		return "", false
	}
	return *p, true
}
