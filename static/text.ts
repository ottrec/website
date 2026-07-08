// normalizeText lowercases and strips diacritics so a search for "lariviere"
// matches "Larivière" (and vice versa). Shared by the map and today filters.
export function normalizeText(s: string): string {
	return s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()
}
