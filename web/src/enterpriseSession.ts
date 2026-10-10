// The Enterprise gateway uses short-lived HttpOnly access cookies. Refresh
// cookies remain HttpOnly and available ONLY at /api/v1/auth. External apps
// must never read/store platform tokens or introduce their own login/session.
let refreshInFlight: Promise<boolean> | null = null
let redirectStarted = false

async function gatewaySessionExpired(response: Response): Promise<boolean> {
  if (response.status !== 401) return false
  const body = await response.clone().json().catch(() => null) as { error?: { code?: string } } | null
  return body?.error?.code === 'UNAUTHENTICATED'
}

function refreshEnterpriseSession(): Promise<boolean> {
  if (refreshInFlight) return refreshInFlight
  refreshInFlight = fetch('/api/v1/auth/refresh', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  }).then(response => response.ok).catch(() => false).finally(() => { refreshInFlight = null })
  return refreshInFlight
}

function signInThroughEnterprise() {
  if (redirectStarted) return
  if (!/^\/apps\/photobooth(?:\/|$)/.test(window.location.pathname)) return
  redirectStarted = true
  const returnTo = window.location.pathname + window.location.search
  // Never include tokens or arbitrary external redirect URLs.
  window.location.assign('/login?return_to=' + encodeURIComponent(returnTo))
}

// Called only after the gateway itself returned UNAUTHENTICATED. Retrying a
// safe-to-replay request once is sufficient: the gateway rejected the first
// attempt before the Photobooth API handled it.
export async function enterpriseFetch(input: string, init: RequestInit): Promise<Response> {
  const response = await fetch(input, init)
  if (!await gatewaySessionExpired(response)) return response
  const refreshed = await refreshEnterpriseSession()
  if (refreshed) return fetch(input, init)
  signInThroughEnterprise()
  return response
}
