import { collectTextSearchMatches, MAX_RENDERED_SEARCH_MATCHES } from '@/lib/jsonSearch'

export interface BodySearchStats {
    matchCount: number
    truncated: boolean
}

export function createTextSearchStats(text: string, term: string): BodySearchStats {
    const result = collectTextSearchMatches(text, term, MAX_RENDERED_SEARCH_MATCHES)
    return { matchCount: result.ranges.length, truncated: result.truncated }
}

export function navigateSearchMatch(container: HTMLElement | null, direction: 'prev' | 'next') {
    if (!container) return;
    const matches = Array.from(container.querySelectorAll<HTMLElement>('[data-search-match]'));
    if (!matches.length) return;
    const prev = container.querySelector<HTMLElement>('[data-search-match-active]');
    if (prev) prev.removeAttribute('data-search-match-active');
    const currentIdx = prev ? matches.indexOf(prev) : -1;
    let nextIdx: number;
    if (direction === 'next') {
        nextIdx = currentIdx + 1 >= matches.length ? 0 : currentIdx + 1;
    } else {
        nextIdx = currentIdx - 1 < 0 ? matches.length - 1 : currentIdx - 1;
    }
    matches[nextIdx].setAttribute('data-search-match-active', '');
    matches[nextIdx].scrollIntoView({ behavior: 'smooth', block: 'nearest' });
}

export function clearActiveSearchMatch(container: HTMLElement | null) {
    container?.querySelector<HTMLElement>('[data-search-match-active]')?.removeAttribute('data-search-match-active');
}
