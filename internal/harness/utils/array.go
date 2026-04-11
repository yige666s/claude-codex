package utils

func Intersperse[T any](items []T, separator func(index int) T) []T {
	if len(items) == 0 {
		return nil
	}
	out := make([]T, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			out = append(out, separator(i))
		}
		out = append(out, item)
	}
	return out
}

func Count[T any](items []T, predicate func(T) bool) int {
	count := 0
	for _, item := range items {
		if predicate(item) {
			count++
		}
	}
	return count
}

func Uniq[T comparable](items []T) []T {
	seen := make(map[T]struct{}, len(items))
	out := make([]T, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
