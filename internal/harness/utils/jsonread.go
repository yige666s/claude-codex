package utils

const UTF8BOM = '\uFEFF'

func StripBOM(content string) string {
	if len(content) > 0 && []rune(content)[0] == UTF8BOM {
		return string([]rune(content)[1:])
	}
	return content
}
