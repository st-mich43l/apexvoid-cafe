import { useCallback, useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { Camera, Coffee, CreditCard, LayoutDashboard, Moon, Plus, RefreshCw, Settings2, ShoppingCart, Sun, TicketCheck, X } from 'lucide-react'

type Item = { id: string; name: string; sku: string; kind: 'drink' | 'photo'; price_vnd: number; active: boolean }
type Order = { id: string; status: 'open' | 'served' | 'cancelled'; total_vnd: number; note: string; created_at: string }
type Booth = { id: string; name: string; active: boolean }
type Booking = { id: string; booth_id: string; booth_name: string; guest_name: string; package_name: string; start: string; end: string; status: 'reserved' | 'checked_in' | 'completed' | 'cancelled'; price_vnd: number }
type Tab = 'overview' | 'counter' | 'booths' | 'catalog'
const base = '/api/apps/cafe/v1'
const money = (value: number) => new Intl.NumberFormat('vi-VN').format(value) + ' ₫'
const timeText = (value: string) => new Date(value).toLocaleString('vi-VN', { dateStyle: 'short', timeStyle: 'short' })

async function api<T>(path: string, payload?: unknown): Promise<T> {
  const response = await fetch(base + path, {
    method: payload === undefined ? 'GET' : 'POST',
    credentials: 'same-origin',
    headers: { Accept: 'application/json', ...(payload !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: payload === undefined ? undefined : JSON.stringify(payload),
  })
  if (!response.ok) {
    const errorBody = await response.json().catch(() => null) as { error?: { code?: string } } | null
    const code = errorBody?.error?.code ?? 'REQUEST_FAILED'
    throw new Error(response.status === 409 ? 'The item or time slot conflicts with an existing record.' :
      response.status === 403 ? 'You do not have permission for this operation.' :
      response.status === 503 ? 'Enterprise authorization or the café service is unavailable.' :
      `${code} (HTTP ${response.status})`)
  }
  return response.json() as Promise<T>
}
const appTabs: { key: Tab; text: string; icon: typeof Coffee }[] = [
  { key: 'overview', text: 'Overview', icon: LayoutDashboard },
  { key: 'counter', text: 'Café counter', icon: Coffee },
  { key: 'booths', text: 'Photo booth', icon: Camera },
  { key: 'catalog', text: 'Menu setup', icon: Settings2 },
]
function Pane({ title, children, extra }: { title: string; children: ReactNode; extra?: React.ReactNode }) {
  return <section className="pane"><header className="pane-head"><h2>{title}</h2>{extra}</header><div className="pane-body">{children}</div></section>
}
function Pill({ value }: { value: string }) {
  return <span className={`pill pill-${value}`}>{value.replaceAll('_', ' ')}</span>
}
function Banner({ message, onClose }: { message: string | null; onClose: () => void }) {
  return message && <div role="alert" className="banner"><span>{message}</span><button aria-label="Dismiss message" onClick={onClose}><X size={15} /></button></div>
}
export function App() {
  const [tab, setTab] = useState<Tab>('overview')
  const [dark, setDark] = useState(() => localStorage.getItem('apexvoid.cafe.theme') !== 'light')
  const [items, setItems] = useState<Item[]>([])
  const [orders, setOrders] = useState<Order[]>([])
  const [booths, setBooths] = useState<Booth[]>([])
  const [bookings, setBookings] = useState<Booking[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [cart, setCart] = useState<Record<string, number>>({})
  const [orderNote, setOrderNote] = useState('')
  const [newItem, setNewItem] = useState({ name: '', sku: '', kind: 'drink' as Item['kind'], price_vnd: '' })
  const [boothName, setBoothName] = useState('')
  const [reservation, setReservation] = useState({ booth_id: '', package_id: '', guest_name: '', start: '', duration: '20' })
  const drinks = useMemo(() => items.filter(item => item.active && item.kind === 'drink'), [items])
  const photos = useMemo(() => items.filter(item => item.active && item.kind === 'photo'), [items])
  const cartItems = useMemo(() => drinks.filter(item => cart[item.id] > 0), [drinks, cart])
  const cartTotal = cartItems.reduce((sum, item) => sum + item.price_vnd * cart[item.id], 0)
  const activeBookings = bookings.filter(booking => booking.status === 'reserved' || booking.status === 'checked_in')
  const load = useCallback(async () => {
    setLoading(true)
    const jobs = await Promise.allSettled([api<Item[]>('/menu'), api<Order[]>('/orders'), api<Booth[]>('/booths'), api<Booking[]>('/bookings')])
    const setters = [setItems, setOrders, setBooths, setBookings] as const
    const failed: string[] = []
    jobs.forEach((result, index) => {
      if (result.status === 'fulfilled') {
        // All returned collection payloads are arrays; update their matching state.
        const arr = result.value
        if (index === 0) setters[0](arr as Item[])
        if (index === 1) setters[1](arr as Order[])
        if (index === 2) setters[2](arr as Booth[])
        if (index === 3) setters[3](arr as Booking[])
      } else failed.push(['Menu', 'Orders', 'Booths', 'Bookings'][index])
    })
    setError(failed.length ? `Unable to load: ${failed.join(', ')}. Check your workspace permissions.` : null)
    setLoading(false)
  }, [])
  useEffect(() => { void load() }, [load])
  useEffect(() => { document.documentElement.dataset.theme = dark ? 'dark' : 'light'; localStorage.setItem('apexvoid.cafe.theme', dark ? 'dark' : 'light') }, [dark])
  async function mutate<T>(job: () => Promise<T>, success: string): Promise<boolean> {
    setBusy(true); setError(null); setNotice(null)
    try { await job(); setNotice(success); await load(); return true }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Request failed'); return false }
    finally { setBusy(false) }
  }
  function makeOrder(event: FormEvent) {
    event.preventDefault()
    if (!cartItems.length) return
    void mutate(() => api('/orders', { note: orderNote, lines: cartItems.map(item => ({ item_id: item.id, quantity: cart[item.id] })) }), 'Order opened. Mark it served after preparing the drinks.')
      .then(ok => { if (ok) { setCart({}); setOrderNote('') } })
  }
  function addItem(event: FormEvent) {
    event.preventDefault()
    const price = Number(newItem.price_vnd)
    if (!Number.isSafeInteger(price) || price < 0) { setError('Price must be a valid integer number of VND.'); return }
    void mutate(() => api('/menu', { ...newItem, price_vnd: price }), 'New menu item created.')
      .then(ok => { if (ok) setNewItem({ name: '', sku: '', kind: 'drink', price_vnd: '' }) })
  }
  function book(event: FormEvent) {
    event.preventDefault()
    const start = new Date(reservation.start)
    if (!Number.isFinite(start.getTime())) { setError('Choose a valid session start time.'); return }
    const end = new Date(start.getTime() + Number(reservation.duration) * 60_000)
    void mutate(() => api('/bookings', { booth_id: reservation.booth_id, package_id: reservation.package_id, guest_name: reservation.guest_name, start: start.toISOString(), end: end.toISOString() }), 'Photo booth reserved.')
      .then(ok => { if (ok) setReservation(current => ({ ...current, guest_name: '' })) })
  }
  return <div className="app-shell">
    <aside className="sidebar">
      <div className="brand"><div className="brand-icon"><Coffee size={23} /></div><div><b>ApexVoid</b><small>CAFÉ & PHOTO</small></div></div>
      <div className="side-label">WORKSPACE</div>
      <nav aria-label="Café navigation">{appTabs.map(item => <button key={item.key} onClick={() => setTab(item.key)} aria-current={tab === item.key ? 'page' : undefined} className={tab === item.key ? 'nav-active' : ''}><item.icon size={18} />{item.text}</button>)}</nav>
      <div className="side-spacer" />
      <div className="side-bottom"><div className="status-dot" />Connected via ApexVoid Enterprise</div>
    </aside>
    <main className="main">
      <header className="topbar"><div><div className="crumb">Business applications <span>/</span> Café & Photo Booth</div><h1>{appTabs.find(t => t.key === tab)?.text}</h1></div>
        <div className="top-actions"><button className="icon-btn" title="Refresh" aria-label="Refresh data" onClick={() => void load()}><RefreshCw size={18} /></button><button className="icon-btn" title="Toggle color mode" aria-label="Toggle color mode" onClick={() => setDark(current => !current)}>{dark ? <Sun size={18} /> : <Moon size={18} />}</button></div>
      </header>
      <div className="content"><Banner message={error} onClose={() => setError(null)} />{notice && <div className="notice">{notice}</div>}
        {loading && <div className="loading">Synchronizing café workspace…</div>}
        {tab === 'overview' && <>
          <div className="intro"><div className="eyebrow">YOUR BUSINESS AT A GLANCE</div><h2>Good coffee. Great memories.</h2><p>Run the café counter and photo-booth sessions together, with centralized access through ApexVoid Enterprise.</p></div>
          <div className="stats"><div><span>Menu items</span><strong>{items.filter(x => x.active).length}</strong><small>Coffee & photo packages</small><Coffee size={21} /></div><div><span>Open orders</span><strong>{orders.filter(x => x.status === 'open').length}</strong><small>Awaiting fulfillment</small><ShoppingCart size={21} /></div><div><span>Photo booths</span><strong>{booths.filter(x => x.active).length}</strong><small>Configured stations</small><Camera size={21} /></div><div><span>Active bookings</span><strong>{activeBookings.length}</strong><small>Reserved or checked in</small><TicketCheck size={21} /></div></div>
          <div className="columns"><Pane title="Recent café orders" extra={<button className="text-action" onClick={() => setTab('counter')}>Open counter →</button>}>{orders.length ? orders.slice(0, 6).map(o => <div className="record" key={o.id}><div><b>Order #{o.id.slice(0, 8)}</b><small>{timeText(o.created_at)}</small></div><div className="record-right"><b>{money(o.total_vnd)}</b><Pill value={o.status} /></div></div>) : <p className="empty">No orders yet. Add drinks to your menu and start serving.</p>}</Pane>
          <Pane title="Photo-booth schedule" extra={<button className="text-action" onClick={() => setTab('booths')}>View bookings →</button>}>{bookings.length ? bookings.slice(0, 6).map(b => <div className="record" key={b.id}><div><b>{b.guest_name}</b><small>{b.booth_name} · {timeText(b.start)}</small></div><Pill value={b.status} /></div>) : <p className="empty">No bookings yet. Configure a booth and a photo package.</p>}</Pane></div>
        </>}
        {tab === 'catalog' && <div className="columns"><Pane title="Menu and photo packages"><div className="items-grid">{items.map(item => <div key={item.id} className="menu-tile"><div className={'tile-icon ' + item.kind}>{item.kind === 'drink' ? <Coffee size={22} /> : <Camera size={22} />}</div><b>{item.name}</b><small>{item.sku} · {item.kind}</small><strong>{money(item.price_vnd)}</strong></div>)}</div>{!items.length && <p className="empty">Create your first drink or photo package.</p>}</Pane><Pane title="Add a menu item"><form className="form" onSubmit={addItem}><label>Item name<input required maxLength={160} value={newItem.name} onChange={e => setNewItem(p => ({ ...p, name: e.target.value }))} placeholder="Iced latte" /></label><label>SKU<input required maxLength={64} value={newItem.sku} onChange={e => setNewItem(p => ({ ...p, sku: e.target.value }))} placeholder="CAFE-LATTE" /></label><label>Product type<select value={newItem.kind} onChange={e => setNewItem(p => ({ ...p, kind: e.target.value as Item['kind'] }))}><option value="drink">Café drink</option><option value="photo">Photo session package</option></select></label><label>Price (VND)<input type="number" required min={0} max={100000000} step={1} value={newItem.price_vnd} onChange={e => setNewItem(p => ({ ...p, price_vnd: e.target.value }))} /></label><button className="primary" disabled={busy}><Plus size={17} />Add item</button></form></Pane></div>}
        {tab === 'counter' && <div className="counter-grid"><Pane title="Café menu" extra={<span className="subtle">Prices in VND</span>}><div className="items-grid">{drinks.map(item => <button key={item.id} className="menu-tile selectable" onClick={() => setCart(current => ({ ...current, [item.id]: Math.min(99, (current[item.id] ?? 0) + 1) }))}><div className="tile-icon drink"><Coffee size={22} /></div><b>{item.name}</b><small>{item.sku}</small><strong>{money(item.price_vnd)}</strong><span className="add-hint"><Plus size={13} /> Add</span></button>)}</div>{!drinks.length && <p className="empty">Add drinks in Menu setup to begin.</p>}</Pane>
            <div className="stack"><Pane title="Current cart"><form className="form" onSubmit={makeOrder}>{cartItems.length ? cartItems.map(item => <div className="cart-line" key={item.id}><div><b>{item.name}</b><small>{money(item.price_vnd)}</small></div><div className="quantity"><button type="button" aria-label={'Remove ' + item.name} onClick={() => setCart(current => ({ ...current, [item.id]: Math.max(0, current[item.id] - 1) }))}>−</button><span>{cart[item.id]}</span><button type="button" aria-label={'Add ' + item.name} onClick={() => setCart(current => ({ ...current, [item.id]: Math.min(99, current[item.id] + 1) }))}>+</button></div></div>) : <p className="empty">Pick a drink to start an order.</p>}<div className="total"><span>Order total</span><b>{money(cartTotal)}</b></div><label>Order note<input value={orderNote} maxLength={300} onChange={e => setOrderNote(e.target.value)} placeholder="Takeaway / table number" /></label><button className="primary" disabled={!cartItems.length || busy}><ShoppingCart size={17} />Create order</button></form></Pane>
              <Pane title="Recent orders">{orders.map(o => <div className="record" key={o.id}><div><b>#{o.id.slice(0, 8)} · {money(o.total_vnd)}</b><small>{timeText(o.created_at)}</small></div>{o.status === 'open' ? <div className="row-actions"><button disabled={busy} onClick={() => void mutate(() => api('/orders/' + o.id + '/serve', {}), 'Order marked served.')}>Served</button><button disabled={busy} onClick={() => void mutate(() => api('/orders/' + o.id + '/cancel', {}), 'Order cancelled.')}>Cancel</button></div> : <Pill value={o.status} />}</div>)}{!orders.length && <p className="empty">No orders yet.</p>}</Pane></div>
          </div>}
        {tab === 'booths' && <div className="columns"><div className="stack"><Pane title="Photo booth stations"><div className="stations">{booths.map(b => <div className="station" key={b.id}><div className="tile-icon photo"><Camera size={21} /></div><div><b>{b.name}</b><small>{b.active ? 'Active station' : 'Inactive'}</small></div></div>)}</div><form className="inline-form" onSubmit={e => { e.preventDefault();void mutate(() => api('/booths', { name: boothName }), 'Booth created.').then(ok => { if (ok) setBoothName('') }) }}><input value={boothName} maxLength={100} required onChange={e => setBoothName(e.target.value)} placeholder="New station name" /><button className="primary" disabled={busy}><Plus size={16} />Add booth</button></form></Pane>
            <Pane title="Upcoming and recent sessions">{bookings.map(b => <div key={b.id} className="booking"><div className="booking-time"><Camera size={17} /><span>{timeText(b.start)}</span></div><div className="booking-row"><div><b>{b.guest_name}</b><small>{b.booth_name} · {b.package_name} · {money(b.price_vnd)}</small></div><Pill value={b.status} /></div>{b.status === 'reserved' && <div className="row-actions"><button disabled={busy} onClick={() => void mutate(() => api('/bookings/' + b.id + '/check-in', {}), 'Guest checked in.')}>Check in</button><button disabled={busy} onClick={() => void mutate(() => api('/bookings/' + b.id + '/cancel', {}), 'Reservation cancelled.')}>Cancel</button></div>}{b.status === 'checked_in' && <div className="row-actions"><button disabled={busy} onClick={() => void mutate(() => api('/bookings/' + b.id + '/complete', {}), 'Session completed.')}>Complete</button></div>}</div>)}{!bookings.length && <p className="empty">No photo sessions yet.</p>}</Pane></div>
            <Pane title="Reserve a photo session"><form className="form" onSubmit={book}><label>Booth<select required value={reservation.booth_id} onChange={e => setReservation(p => ({ ...p, booth_id: e.target.value }))}><option value="">Select booth</option>{booths.filter(x => x.active).map(x => <option key={x.id} value={x.id}>{x.name}</option>)}</select></label><label>Photo package<select required value={reservation.package_id} onChange={e => setReservation(p => ({ ...p, package_id: e.target.value }))}><option value="">Select package</option>{photos.map(x => <option key={x.id} value={x.id}>{x.name} · {money(x.price_vnd)}</option>)}</select></label><label>Guest / group name<input required maxLength={120} value={reservation.guest_name} onChange={e => setReservation(p => ({ ...p, guest_name: e.target.value }))} placeholder="Customer name" /></label><label>Start date & time<input type="datetime-local" required value={reservation.start} onChange={e => setReservation(p => ({ ...p, start: e.target.value }))} /></label><label>Duration<select value={reservation.duration} onChange={e => setReservation(p => ({ ...p, duration: e.target.value }))}>{[15,20,30,45,60].map(n => <option key={n} value={n}>{n} minutes</option>)}</select></label><p className="helper">Booked times are checked by PostgreSQL. Conflicting sessions cannot overlap.</p><button className="primary" disabled={busy || !photos.length || !booths.length}><TicketCheck size={17} />Reserve session</button></form></Pane></div>}
      </div>
      <footer><span>ApexVoid Café · Independent application</span><span><CreditCard size={13} /> Orders are not payment receipts</span></footer>
    </main>
  </div>
}
