import { useState, useCallback, useEffect, useMemo, useReducer, useRef } from 'react'
import { useParams, Link } from 'react-router-dom'
import { Link2, RefreshCw, ChevronLeft, ChevronRight, ArrowUpDown, Loader2, Search, X } from 'lucide-react'
import * as api from '../api/client'
import type { GroupResponse, GroupsPage, LibraryStatus } from '../api/client'
import { usePolling } from '../api/usePolling'
import GroupCard from '../components/GroupCard'
import LibraryCard from '../components/LibraryCard'
import UnknownCard from '../components/UnknownCard'
import PipelineInfoBar from '../components/PipelineInfoBar'

const PAGE_SIZE = 12

type PendingSort = 'number' | 'size' | 'date' | 'status'

const STATUS_ORDER: Record<string, number> = {
  downloading: 0,
  scraping: 1,
  failed: 2,
  ready: 3,
  linking: 4,
  merging: 4,
  error: 5,
}

function getGroupStatusKey(g: GroupResponse): string {
  if (g.task === 'linking' || g.task === 'merging') return g.task
  if (g.task === 'error') return 'error'
  if (!g.allReady) return 'downloading'
  if (g.scrape.status === 'scraping') return 'scraping'
  if (g.scrape.status === 'failed') return 'failed'
  return 'ready'
}

type LibState = {
  page: number
  data: api.LibraryPage | null
  loading: boolean
  sort: string
  sortDir: 'asc' | 'desc'
  status: LibraryStatus
  q: string
}

type LibAction =
  | { type: 'setPage'; value: number }
  | { type: 'setData'; value: api.LibraryPage | null }
  | { type: 'setLoading'; value: boolean }
  | { type: 'toggleSort'; key: string }
  | { type: 'setStatus'; value: LibraryStatus }
  | { type: 'setQuery'; value: string }

const initialLib: LibState = {
  page: 0,
  data: null,
  loading: false,
  sort: 'added',
  sortDir: 'desc',
  status: 'all',
  q: '',
}

function libReducer(state: LibState, action: LibAction): LibState {
  switch (action.type) {
    case 'setPage': return { ...state, page: action.value }
    case 'setData': return { ...state, data: action.value }
    case 'setLoading': return { ...state, loading: action.value }
    case 'toggleSort': {
      if (state.sort === action.key) {
        return { ...state, sortDir: state.sortDir === 'asc' ? 'desc' : 'asc' }
      }
      return { ...state, sort: action.key, sortDir: 'desc', page: 0 }
    }
    case 'setStatus': return { ...state, status: action.value, page: 0 }
    case 'setQuery': return { ...state, q: action.value, page: 0 }
  }
}

export default function PipelineDetail() {
  const { id: idStr } = useParams<{ id: string }>()
  const pipelineId = Number(idStr)
  const [tab, setTab] = useState<'pending' | 'library'>('pending')

  // Pipeline info
  const pipesFetcher = useCallback(() => api.listPipelines(), [])
  const { data: pipes } = usePolling(pipesFetcher, 10000)
  const pipeline = pipes?.find((p) => p.id === pipelineId)

  // Groups (polling)
  const groupsFetcher = useCallback(() => api.listGroups(pipelineId), [pipelineId])
  const { data: groupsPage, loading: groupsLoading, refresh: refreshGroups } = usePolling(groupsFetcher, 3000)

  // Auto-scan when Pending tab is activated
  const lastScanRef = useRef(0)
  useEffect(() => {
    if (tab !== 'pending') return
    const now = Date.now()
    if (now - lastScanRef.current < 5000) return // debounce 5s
    lastScanRef.current = now
    api.triggerScan(pipelineId).catch(() => {})
  }, [tab, pipelineId])

  // Per-group selection state, adjusted during render when new group data
  // arrives (https://react.dev/learn/you-might-not-need-an-effect).
  const [selections, setSelections] = useState<Map<string, Set<string>>>(new Map())
  const [syncedGroupsPage, setSyncedGroupsPage] = useState<GroupsPage | null>(null)

  if (groupsPage && groupsPage !== syncedGroupsPage) {
    setSyncedGroupsPage(groupsPage)
    setSelections(prev => {
      const next = new Map(prev)
      const currentNumbers = new Set(groupsPage.groups.map(g => g.number))
      for (const key of next.keys()) {
        if (!currentNumbers.has(key)) next.delete(key)
      }
      for (const g of groupsPage.groups) {
        if (!next.has(g.number)) {
          const readyPaths = new Set<string>()
          for (const i of g.items) {
            if (i.ready) readyPaths.add(i.path)
          }
          next.set(g.number, readyPaths)
        }
      }
      return next
    })
  }

  const handleSelectionChange = useCallback((number: string, selected: Set<string>) => {
    setSelections(prev => {
      const next = new Map(prev)
      next.set(number, selected)
      return next
    })
  }, [])

  // Library state (server-paginated)
  const [lib, dispatchLib] = useReducer(libReducer, initialLib)

  const fetchLibrary = useCallback(async () => {
    dispatchLib({ type: 'setLoading', value: true })
    try {
      const data = await api.listLibrary(pipelineId, lib.page, PAGE_SIZE, lib.sort, lib.sortDir, lib.status, lib.q)
      dispatchLib({ type: 'setData', value: data })
    } catch { /* ignore */ }
    dispatchLib({ type: 'setLoading', value: false })
  }, [pipelineId, lib.page, lib.sort, lib.sortDir, lib.status, lib.q])

  // Library data is server-paginated, so re-fetch whenever the tab is open
  // and any of page/sort/sortDir/status/pipelineId changes. fetchLibrary is
  // a useCallback whose identity changes on those deps, so this effect fires
  // on tab switch, pagination, sort toggle, and status filter changes.
  useEffect(() => {
    if (tab !== 'library') return
    void fetchLibrary()
  }, [tab, fetchLibrary])

  const handleTabSwitch = (next: 'pending' | 'library') => {
    setTab(next)
  }

  const handleScan = async () => {
    lastScanRef.current = Date.now()
    try { await api.triggerScan(pipelineId) } catch { /* */ }
  }

  if (!pipeline) {
    return <div className="flex justify-center py-12"><Loader2 size={24} className="animate-spin text-gray-500" /></div>
  }

  return (
    <div className="pb-32">
      {/* Breadcrumb */}
      <div className="flex items-center gap-2 text-sm mb-4">
        <Link to="/" className="text-gray-500 hover:text-white transition">bangou</Link>
        <span className="text-gray-700">›</span>
        <span className="text-white font-medium">{pipeline.name}</span>
      </div>

      <PipelineInfoBar pipeline={pipeline} />

      {/* Tabs */}
      <div className="flex items-center gap-1 mb-4 border-b border-gray-800">
        <button type="button" onClick={() => handleTabSwitch('pending')}
          className={`px-4 py-2 text-sm transition ${tab === 'pending' ? 'text-white border-b-2 border-indigo-500' : 'text-gray-500 hover:text-gray-300'}`}>
          Pending ({(groupsPage?.groups.length || 0) + (groupsPage?.unknowns.length || 0)})
        </button>
        <button type="button" onClick={() => handleTabSwitch('library')}
          className={`px-4 py-2 text-sm transition ${tab === 'library' ? 'text-white border-b-2 border-indigo-500' : 'text-gray-500 hover:text-gray-300'}`}>
          Library ({lib.data?.total ?? pipeline.libraryCount})
        </button>
      </div>

      {tab === 'pending' && (
        <PendingTab
          pipelineId={pipelineId}
          groupsPage={groupsPage}
          groupsLoading={groupsLoading}
          selections={selections}
          onSelectionChange={handleSelectionChange}
          onAction={refreshGroups}
          onScan={handleScan}
        />
      )}

      {tab === 'library' && (
        <LibraryTab
          lib={lib}
          dispatchLib={dispatchLib}
          onAction={fetchLibrary}
        />
      )}
    </div>
  )
}

function PendingTab({ pipelineId, groupsPage, groupsLoading, selections, onSelectionChange, onAction, onScan }: {
  pipelineId: number
  groupsPage: GroupsPage | null
  groupsLoading: boolean
  selections: Map<string, Set<string>>
  onSelectionChange: (number: string, selected: Set<string>) => void
  onAction: () => void
  onScan: () => void
}) {
  const [pendingPage, setPendingPage] = useState(0)
  const [pendingSort, setPendingSort] = useState<PendingSort>('status')
  const [pendingSortDir, setPendingSortDir] = useState<'asc' | 'desc'>('asc')
  const [laProgress, setLaProgress] = useState<{ total: number; done: number; current: string; running: boolean } | null>(null)

  const allPendingItems = useMemo(() => {
    const groups = groupsPage?.groups || []
    const unknowns = groupsPage?.unknowns || []
    return { groups, unknowns, total: groups.length + unknowns.length }
  }, [groupsPage])

  const sortedPendingItems = useMemo(() => {
    const groups = allPendingItems.groups.toSorted((a, b) => {
      let cmp = 0
      switch (pendingSort) {
        case 'number': cmp = a.number.localeCompare(b.number); break
        case 'size': cmp = a.totalSizeGB - b.totalSizeGB; break
        case 'date': {
          const da = a.scrape.meta?.premiered || a.scrape.meta?.year || ''
          const db = b.scrape.meta?.premiered || b.scrape.meta?.year || ''
          cmp = da.localeCompare(db)
          break
        }
        case 'status': cmp = (STATUS_ORDER[getGroupStatusKey(a)] ?? 99) - (STATUS_ORDER[getGroupStatusKey(b)] ?? 99); break
      }
      return pendingSortDir === 'asc' ? cmp : -cmp
    })
    return [
      ...groups.map((g) => ({ type: 'group' as const, data: g })),
      ...allPendingItems.unknowns.map((u) => ({ type: 'unknown' as const, data: u })),
    ]
  }, [allPendingItems, pendingSort, pendingSortDir])

  const pendingTotalPages = Math.ceil(sortedPendingItems.length / PAGE_SIZE)
  const pendingSlice = sortedPendingItems.slice(pendingPage * PAGE_SIZE, (pendingPage + 1) * PAGE_SIZE)

  const togglePendingSort = (key: PendingSort) => {
    if (pendingSort === key) setPendingSortDir(pendingSortDir === 'asc' ? 'desc' : 'asc')
    else { setPendingSort(key); setPendingSortDir(key === 'status' || key === 'number' ? 'asc' : 'desc') }
    setPendingPage(0)
  }

  // Link All (frontend-driven, current page only) — single pass filter
  const linkableGroups = useMemo(() => {
    const out: { type: 'group'; data: GroupResponse }[] = []
    for (const item of pendingSlice) {
      if (item.type !== 'group') continue
      if (isGroupLinkEligible(item.data, selections.get(item.data.number) ?? new Set())) {
        out.push(item as { type: 'group'; data: GroupResponse })
      }
    }
    return out
  }, [pendingSlice, selections])

  const linkableCount = linkableGroups.length

  const handleLinkAll = async () => {
    const groups = linkableGroups.map(item => ({
      number: item.data.number,
      paths: [...(selections.get(item.data.number) ?? new Set())],
    }))
    if (groups.length === 0) return

    setLaProgress({ total: groups.length, done: 0, current: '', running: true })

    // Run all independent link requests concurrently.
    await Promise.all(groups.map(async (g, i) => {
      setLaProgress({ total: groups.length, done: i, current: g.number, running: true })
      try {
        await api.groupLink(pipelineId, g.number, g.paths)
      } catch { /* continue */ }
    }))

    setLaProgress({ total: groups.length, done: groups.length, current: '', running: false })
    onAction()
    setTimeout(() => setLaProgress(null), 3000)
  }

  return (
    <>
      {allPendingItems.total > 0 && (
        <div className="flex items-center gap-2 mb-4">
          <ArrowUpDown size={12} className="text-gray-600" />
          {(['number', 'size', 'date', 'status'] as PendingSort[]).map((key) => (
            <button type="button" key={key} onClick={() => togglePendingSort(key)}
              className={`text-xs px-2.5 py-1 rounded-lg transition-all duration-200 ${pendingSort === key ? 'bg-indigo-600 text-white shadow-sm shadow-indigo-500/20' : 'bg-[#1a1a1a] text-gray-500 hover:text-white border border-gray-800'}`}>
              {key}{pendingSort === key && (pendingSortDir === 'desc' ? ' ↓' : ' ↑')}
            </button>
          ))}
          <span className="text-xs text-gray-600 ml-auto">
            {allPendingItems.total} groups
          </span>
        </div>
      )}

      {groupsLoading && allPendingItems.total === 0 ? (
        <div className="flex justify-center py-12"><Loader2 size={24} className="animate-spin text-gray-500" /></div>
      ) : allPendingItems.total > 0 ? (
        <>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {pendingSlice.map((item) =>
              item.type === 'group'
                ? <GroupCard key={item.data.number} group={item.data} pipelineId={pipelineId} onAction={onAction}
                    selected={selections.get(item.data.number) ?? new Set()}
                    onSelectionChange={(sel) => onSelectionChange(item.data.number, sel)} />
                : <UnknownCard key={item.data.path} file={item.data} pipelineId={pipelineId} onAction={onAction} />
            )}
          </div>
          {pendingTotalPages > 1 && (
            <Pagination page={pendingPage} totalPages={pendingTotalPages} onPage={setPendingPage} />
          )}
        </>
      ) : (
        <div className="text-center py-12">
          <p className="text-gray-500 mb-2">No pending groups</p>
          <p className="text-xs text-gray-700">Files added to the input directory will appear here automatically.</p>
        </div>
      )}

      {/* FABs */}
      <div className="fixed bottom-6 right-6 flex flex-col gap-3 z-40">
        {linkableCount > 0 || laProgress ? (
          laProgress ? (
            <div className="bg-[#1a1a1a] border border-gray-700 rounded-2xl px-4 py-3 shadow-2xl min-w-[180px]">
              {laProgress.running ? (
                <div className="space-y-2">
                  <div className="text-xs text-gray-400">Linking {laProgress.done + 1}/{laProgress.total}</div>
                  <div className="w-full h-1.5 bg-gray-800 rounded-full overflow-hidden">
                    <div className="h-full bg-indigo-500 transition-all duration-500 rounded-full"
                      style={{ width: `${(laProgress.done / laProgress.total) * 100}%` }} />
                  </div>
                </div>
              ) : (
                <div className="text-xs text-emerald-400">Linked {laProgress.done} groups ✓</div>
              )}
            </div>
          ) : (
            <button type="button" onClick={handleLinkAll}
              className="flex items-center gap-2 px-5 py-3 bg-indigo-600 hover:bg-indigo-500 text-white text-sm rounded-2xl shadow-2xl shadow-indigo-500/20 transition">
              <Link2 size={16} />Link All ({linkableCount})
            </button>
          )
        ) : null}
        <button type="button" onClick={onScan}
          className="flex items-center gap-2 px-5 py-3 bg-[#1a1a1a] hover:bg-[#222] border border-gray-700 text-gray-300 hover:text-white text-sm rounded-2xl shadow-2xl transition">
          <RefreshCw size={16} />Rescan
        </button>
      </div>
    </>
  )
}

function LibraryTab({ lib, dispatchLib, onAction }: {
  lib: LibState
  dispatchLib: (action: LibAction) => void
  onAction: () => void
}) {
  // Local input state for the quick filter; debounced into the reducer's `q`
  // so the server query only fires after the user stops typing.
  const [queryInput, setQueryInput] = useState('')
  useEffect(() => {
    if (queryInput === lib.q) return
    const t = setTimeout(() => dispatchLib({ type: 'setQuery', value: queryInput }), 300)
    return () => clearTimeout(t)
  }, [queryInput, lib.q, dispatchLib])

  return (
    <>
      <div className="flex flex-wrap items-center gap-2 mb-4">
        <div className="flex items-center gap-1">
          {(['all', 'alive', 'missing'] as LibraryStatus[]).map((status) => (
            <button type="button" key={status} onClick={() => dispatchLib({ type: 'setStatus', value: status })}
              className={`text-xs px-2.5 py-1 rounded-lg transition-all duration-200 ${lib.status === status ? 'bg-amber-600 text-white shadow-sm shadow-amber-500/20' : 'bg-[#1a1a1a] text-gray-500 hover:text-white border border-gray-800'}`}>
              {status === 'all' ? 'All' : status === 'alive' ? 'Alive' : 'Link Missing'}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-2">
          <ArrowUpDown size={12} className="text-gray-600" />
          {['added', 'number', 'date', 'rating'].map((key) => (
            <button type="button" key={key} onClick={() => dispatchLib({ type: 'toggleSort', key })}
              className={`text-xs px-2.5 py-1 rounded-lg transition-all duration-200 ${lib.sort === key ? 'bg-indigo-600 text-white shadow-sm shadow-indigo-500/20' : 'bg-[#1a1a1a] text-gray-500 hover:text-white border border-gray-800'}`}>
              {key}{lib.sort === key && (lib.sortDir === 'desc' ? ' ↓' : ' ↑')}
            </button>
          ))}
        </div>
        <div className="relative flex-1 min-w-[180px] max-w-xs ml-auto">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-gray-600 pointer-events-none" />
          <input
            type="text"
            value={queryInput}
            onChange={(e) => setQueryInput(e.target.value)}
            placeholder="Search number or title…"
            className="w-full bg-[#1a1a1a] border border-gray-800 rounded-lg pl-8 pr-7 py-1.5 text-xs text-gray-200 placeholder-gray-600 focus:outline-none focus:border-gray-600 transition"
          />
          {queryInput && (
            <button type="button" onClick={() => setQueryInput('')} aria-label="Clear search"
              className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-600 hover:text-white transition">
              <X size={13} />
            </button>
          )}
        </div>
        <span className="text-xs text-gray-600 w-full sm:w-auto sm:ml-0">{lib.data?.total ?? 0} bangous</span>
      </div>

      {lib.loading && !lib.data ? (
        <div className="flex justify-center py-12"><Loader2 size={24} className="animate-spin text-gray-500" /></div>
      ) : lib.data && lib.data.items.length > 0 ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {lib.data.items.map((item) => <LibraryCard key={item.number} item={item} onAction={onAction} />)}
        </div>
      ) : (
        <div className="text-center py-12">
          <p className="text-gray-500 mb-2">{lib.q ? 'No matches' : lib.status === 'missing' ? 'No missing links' : lib.status === 'alive' ? 'No alive links' : 'Library is empty'}</p>
          <p className="text-xs text-gray-700">{lib.q ? 'Try a different search term.' : lib.status === 'all' ? 'Link groups from the Pending tab to build your library.' : 'Change the status filter to see other library entries.'}</p>
        </div>
      )}

      {lib.data && Math.ceil(lib.data.total / PAGE_SIZE) > 1 && (
        <Pagination page={lib.page} totalPages={Math.ceil(lib.data.total / PAGE_SIZE)} onPage={(p) => dispatchLib({ type: 'setPage', value: p })} />
      )}
    </>
  )
}

function Pagination({ page, totalPages, onPage }: { page: number; totalPages: number; onPage: (p: number) => void }) {
  const pages = paginationRange(page, totalPages)
  return (
    <div className="flex items-center justify-center gap-3 mt-6">
      <button type="button" onClick={() => onPage(Math.max(0, page - 1))} disabled={page === 0}
        aria-label="Previous page"
        className="p-2 text-gray-500 hover:text-white disabled:opacity-20 transition"><ChevronLeft size={16} /></button>
      <div className="flex gap-1">
        {pages.map((p, idx) =>
          p === -1 ? (
            <span key={`ellipsis-${idx}`} className="w-8 h-8 flex items-center justify-center text-xs text-gray-600">…</span>
          ) : (
            <button type="button" key={p} onClick={() => onPage(p)}
              className={`w-8 h-8 text-xs rounded-lg transition ${p === page ? 'bg-indigo-600 text-white' : 'bg-[#1a1a1a] text-gray-500 hover:text-white border border-gray-800'}`}>
              {p + 1}
            </button>
          )
        )}
      </div>
      <button type="button" onClick={() => onPage(Math.min(totalPages - 1, page + 1))} disabled={page >= totalPages - 1}
        aria-label="Next page"
        className="p-2 text-gray-500 hover:text-white disabled:opacity-20 transition"><ChevronRight size={16} /></button>
    </div>
  )
}

function isGroupLinkEligible(g: GroupResponse, selected: Set<string>): boolean {
  if (g.scrape.status !== 'success' || g.task || g.items.length === 0 || !g.allReady) return false
  if (selected.size === 0) return false
  const exts = new Set<string>()
  for (const item of g.items) {
    if (!selected.has(item.path)) continue
    const idx = (item.filename || item.path).lastIndexOf('.')
    if (idx < 0) return false
    exts.add((item.filename || item.path).slice(idx).toLowerCase())
    if (exts.size > 1) return false
  }
  return exts.size === 1
}

function paginationRange(current: number, total: number): number[] {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i)
  const pages: number[] = []
  const near = new Set([0, 1, current - 1, current, current + 1, total - 2, total - 1])
  const sorted = [...near].filter((p) => p >= 0 && p < total).sort((a, b) => a - b)
  for (let i = 0; i < sorted.length; i++) {
    if (i > 0 && sorted[i] - sorted[i - 1] > 1) pages.push(-1)
    pages.push(sorted[i])
  }
  return pages
}
