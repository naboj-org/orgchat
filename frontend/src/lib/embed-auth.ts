import { AuthenticationError, request } from './api'

// JWTs exist only in memory during exchange, never in URLs or persistent storage.
export function createEmbedAuth(setVisible: (visible: boolean) => void) {
  let parentOrigin: string | undefined
  let disposed = false
  let cookiesAvailable: boolean | undefined
  let cookieCheck: Promise<boolean> | undefined
  let pending: {
    id: string
    resolve: (token: string) => void
    reject: (error: Error) => void
    timer: ReturnType<typeof setTimeout>
  } | undefined

  function setParentOrigin(origin: string | undefined) {
    parentOrigin = origin
  }

  function handleMessage(event: MessageEvent) {
    if (disposed || event.source !== window.parent || event.origin !== parentOrigin) return
    const data = event.data
    if (data?.type !== 'orgchat:auth-token' || !pending || data.requestId !== pending.id) return
    const current = pending
    pending = undefined
    clearTimeout(current.timer)
    if (typeof data.token !== 'string' || !data.token || data.token.length > 16384) {
      current.reject(new AuthenticationError('The embedding website did not provide a valid sign-in token.'))
    } else current.resolve(data.token)
  }

  async function checkCookies(): Promise<boolean> {
    if (cookiesAvailable !== undefined) return cookiesAvailable
    if (!cookieCheck) cookieCheck = (async () => {
      try {
        const { probe } = await request<{ probe: string }>('/cookie-check', { method: 'POST' })
        const result = await request<{ cookiesAvailable: boolean }>(`/cookie-check?probe=${encodeURIComponent(probe)}`)
        if (!disposed) {
          cookiesAvailable = result.cookiesAvailable
          setVisible(cookiesAvailable)
        }
        return result.cookiesAvailable
      } catch (cause) {
        // A network/server error does NOT prove that cookies are blocked.
        if (!disposed) setVisible(true)
        throw cause
      } finally {
        cookieCheck = undefined
      }
    })()
    return cookieCheck
  }

  let authentication: Promise<void> | undefined

  function authenticate() {
    if (!authentication) authentication = exchange().finally(() => { authentication = undefined })
    return authentication
  }

  async function exchange() {
    if (disposed) throw new AuthenticationError('Chat was closed.')
    if (!parentOrigin || window.parent === window) {
      throw new AuthenticationError('Sign in to orgchat, or open it through a website that provides authentication.')
    }
    if (pending) throw new AuthenticationError('Authentication is already in progress.')
    const token = await new Promise<string>((resolve, reject) => {
      const id = crypto.randomUUID()
      const timer = setTimeout(() => {
        pending = undefined
        reject(new AuthenticationError('The embedding website did not respond to the sign-in request.'))
      }, 30000)
      pending = { id, resolve, reject, timer }
      window.parent.postMessage({ type: 'orgchat:auth-required', requestId: id }, parentOrigin!)
    })
    if (disposed) throw new AuthenticationError('Chat was closed.')
    await request<{ authenticated: boolean }>('/auth/embed', {
      method: 'POST', body: JSON.stringify({ token }),
    })
  }

  function dispose() {
    disposed = true
    if (pending) {
      clearTimeout(pending.timer)
      pending.reject(new AuthenticationError('Chat was closed.'))
      pending = undefined
    }
  }

  return { setParentOrigin, checkCookies, authenticate, handleMessage, dispose }
}
