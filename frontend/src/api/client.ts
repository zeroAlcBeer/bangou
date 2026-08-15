const BASE = '/api'

// errorMessage extracts a display message from an unknown catch value.
export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const opts: RequestInit = {
    method,
    headers: { 'Content-Type': 'application/json' },
  }
  if (body !== undefined) {
    opts.body = JSON.stringify(body)
  }
  const res = await fetch(`${BASE}${path}`, opts)
  const data = await res.json()
  if (!res.ok) {
    throw new Error(data.error || `HTTP ${res.status}`)
  }
  return data as T
}

// ── Pipelines ──

export interface PipelineResponse {
  id: number
  name: string
  inputDir: string
  outputDir: string
  pathPattern: string
  archiveDir: string
  enableMerge: boolean
  downloadProvider: string
  scrapeProviders: string[]
  pendingCount: number
  libraryCount: number
  status: string
}

export interface CreatePipelineReq {
  name: string
  inputDir: string
  outputDir: string
  pathPattern: string
  archiveDir: string
  enableMerge: boolean
  downloadProvider: string
  scrapeProviders: string[]
}

export const listPipelines = () => request<PipelineResponse[]>('GET', '/pipelines')
export const createPipeline = (data: CreatePipelineReq) => request<{ id: number }>('POST', '/pipelines', data)

// ── Groups ──

export interface GroupsPage {
  groups: GroupResponse[]
  unknowns: UnknownResponse[]
}

export interface GroupResponse {
  number: string
  items: ItemResponse[]
  totalSizeGB: number
  scrape: ScrapeResponse
  task: string
  taskErr?: string
  taskProgress: number
  allReady: boolean
}

export interface ItemResponse {
  path: string
  filename: string
  part: number
  sizeGB: number
  ready: boolean
  resolution?: string
  videoCodec?: string
  audioCodec?: string
  bitrate?: string
  duration?: string
  downloadPct: number
  downloadStatus?: string
}

export interface ScrapeResponse {
  meta: MetaResponse | null
  errors?: Record<string, string>
  status: string
}

export interface MetaResponse {
  number: string
  title: string
  director?: string
  maker?: string
  label?: string
  series?: string
  actors?: string[]
  genres?: string[]
  coverURL?: string
  sampleImages?: string[]
  premiered?: string
  year?: string
  runtime?: string
  rating?: string
  reviewCount: number
  sampleMovieURL?: string
  pageURL?: string
  provider?: string
}

export interface UnknownResponse {
  path: string
  filename: string
  sizeGB: number
}

export const listGroups = (pipelineId: number) =>
  request<GroupsPage>('GET', `/pipelines/${pipelineId}/groups`)

// ── Group Actions ──

export const groupLink = (pipelineId: number, number: string, paths: string[]) =>
  request<unknown>('POST', `/pipelines/${pipelineId}/groups/${number}/link`, { paths })

export const groupMerge = (pipelineId: number, number: string, paths: string[]) =>
  request<unknown>('POST', `/pipelines/${pipelineId}/groups/${number}/merge`, { paths })

export const groupRescrape = (pipelineId: number, number: string) =>
  request<unknown>('POST', `/pipelines/${pipelineId}/groups/${number}/rescrape`, {})

export const groupTag = (pipelineId: number, number: string, path: string) =>
  request<unknown>('POST', `/pipelines/${pipelineId}/groups/${number}/tag`, { path })

// ── Unknown Actions ──

export const unknownTag = (pipelineId: number, path: string, number: string) =>
  request<unknown>('POST', `/pipelines/${pipelineId}/unknowns/tag`, { path, number })

// ── Scan ──

export const triggerScan = (pipelineId: number) =>
  request<unknown>('POST', `/pipelines/${pipelineId}/scan`, {})

// ── Library ──

export interface LibraryPage {
  items: BangouResponse[]
  total: number
  page: number
  size: number
}

export interface BangouFileResponse {
  id: number
  srcPath: string
  linkPath: string
  linkType: string
  fileSize: number
  resolution?: string
  videoCodec?: string
  audioCodec?: string
  duration?: string
  bitrate?: string
  alive: boolean
  sourceAvailable: boolean
}

export interface BangouResponse {
  id: number
  number: string
  outputs: BangouFileResponse[]
  nfoPath?: string
  coverPath?: string
  rawPath?: string
  title?: string
  actors?: string
  genres?: string[]
  coverURL?: string
  sampleImages?: string[]
  rating?: string
  reviewCount: number
  pageURL?: string
  maker?: string
  label?: string
  series?: string
  director?: string
  sampleMovieURL?: string
  premiered?: string
  year?: string
  runtime?: string
  provider?: string
}

export type LibraryStatus = 'all' | 'alive' | 'missing'

export const listLibrary = (pipelineId: number, page = 0, size = 12, sort = 'added', order = 'desc', status: LibraryStatus = 'all', q = '') =>
  request<LibraryPage>('GET', `/pipelines/${pipelineId}/library?page=${page}&size=${size}&sort=${sort}&order=${order}&status=${status}&q=${encodeURIComponent(q)}`)

// ── Library Actions ──

export const libraryRescrape = (number: string) =>
  request<unknown>('POST', `/library/${number}/rescrape`, {})

export const unlinkBangou = (bangouId: number) =>
  request<unknown>('POST', `/bangous/${bangouId}/unlink`, {})

export const restoreBangou = (bangouId: number) =>
  request<unknown>('POST', `/bangous/${bangouId}/restore`, {})

export const backToPendingBangou = (bangouId: number) =>
  request<unknown>('POST', `/bangous/${bangouId}/back-to-pending`, {})

// ── Provider Configs ──

export interface ProviderConfig {
  provider: string
  config: string
}

export const listProviderConfigs = () => request<ProviderConfig[]>('GET', '/provider-configs')

export const setProviderConfig = (provider: string, config: Record<string, string>) =>
  request<unknown>('PUT', `/provider-configs/${provider}`, config)

export const testProviderConfig = (provider: string) =>
  request<{ status: string }>('POST', `/provider-configs/${provider}/test`, {})

// ── Directory Browse ──

export interface BrowseEntry {
  name: string
  path: string
  isDir: boolean
}

export const browseDirectory = (path: string) =>
  request<BrowseEntry[]>('GET', `/browse?path=${encodeURIComponent(path)}`)
