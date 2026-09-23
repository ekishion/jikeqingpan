package web

import "embed"

// FS 嵌入所有前端静态资源（HTML / CSS / JS 等）。
//
//go:embed static/*
var FS embed.FS
