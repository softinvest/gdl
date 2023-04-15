package gdl

import (
    "mime"
    "net/url"
    "path/filepath"
    "strings"
)

var DefaultFileName = "gdl.output"

func GetFilename(URL string) string {
    if u, err := url.Parse(URL); err == nil && filepath.Ext(u.Path) != "" {
        return filepath.Base(u.Path)
    }

    return DefaultFileName
}

func getNameFromHeader(val string) string {
    _, params, err := mime.ParseMediaType(val)
    if err != nil || strings.Contains(params["filename"], "..") || strings.Contains(params["filename"], "/") || strings.Contains(params["filename"], "\\") {
        return ""
    }

    return params["filename"]
}
