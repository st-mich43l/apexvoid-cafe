import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { CalendarDays, Camera, Check, ChevronLeft, ChevronRight, Clock3, Coffee, History, LayoutDashboard, Moon, Plus, RefreshCw, Search, Settings2, Sun, TicketCheck, UserRound, X } from 'lucide-react'

type Item = { id: string; name: string; sku: string; kind: 'drink' | 'photo'; price_vnd: number; duration_minutes: number; active: boolean }
type Order = { id: string; status: 'open' | 'served' | 'cancelled'; total_vnd: number; note: string; created_at: string }
type Booth = { id: string; name: string; active: boolean }
type BookingStatus = 'confirmed' | 'checked_in' | 'in_progress' | 'completed' | 'cancelled' | 'no_show'
type Booking = { id: string; booking_ref: string; booth_id: string; booth_name: string; package_id: string; guest_name: string; guest_phone?: string; guest_email?: string; package_name: string; start: string; end: string; status: BookingStatus; price_vnd: number; party_size: number; notes?: string; created_at: string; updated_at: string }
type BookingEvent = { id: string; event_type: string; from_status?: string; to_status?: string; reason?: string; changes?: Record<string, unknown>; created_at: string; actor_id: string }
type Schedule = { id: string; booth_id?: string; weekday: number; open_time: string; close_time: string; closed: boolean; slot_increment_minutes: number; min_advance_minutes: number; max_horizon_days: number; timezone: string }
type Blackout = { id: string; booth_id?: string; start: string; end: string; reason: string }
type Page<T> = { items: T[]; page: number; page_size: number; total: number; has_more: boolean }
type Tab = 'overview' | 'bookings' | 'history' | 'counter' | 'setup'
type AppContext = { application_id: string; display_name: string }

const base = '/api/apps/photobooth/v1'
const fallbackAppContext: AppContext = { application_id: 'photobooth', display_name: 'ApexVoid Photobooth' }
const money = (value: number) => new Intl.NumberFormat('vi-VN').format(value) + ' ₫'
const timeText = (value: string) => new Date(value).toLocaleString('vi-VN', { timeZone: 'Asia/Ho_Chi_Minh', dateStyle: 'short', timeStyle: 'short' })
const bookingDateTimeInput = (value: string) => {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Ho_Chi_Minh', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(value))
  const part = (type: string) => parts.find(item => item.type === type)?.value ?? '00'
  return `${part('year')}-${part('month')}-${part('day')}T${part('hour')}:${part('minute')}`
}
const vietnamLocalToISO = (value: string) => {
  if (!value) return ''
  const date = new Date(`${value}:00+07:00`)
  return Number.isFinite(date.getTime()) ? date.toISOString() : ''
}
const dateKey = (date: Date) => { const y = date.getFullYear(); const m = String(date.getMonth() + 1).padStart(2, '0'); const d = String(date.getDate()).padStart(2, '0'); return `${y}-${m}-${d}` }
const dateLabel = (date: Date) => date.toLocaleDateString('vi-VN', { weekday: 'short', day: 'numeric', month: 'short' })
const statusLabel = (status: string) => status.replaceAll('_', ' ')

async function api<T>(path: string, options: { method?: string; body?: unknown } = {}): Promise<T> {
  const selectedWorkspace = new URLSearchParams(window.location.search).get('workspace_id') ?? window.localStorage.getItem('apexvoid.active_workspace')
  const response = await fetch(base + path, {
    method: options.method ?? (options.body === undefined ? 'GET' : 'POST'),
    credentials: 'same-origin',
    headers: { Accept: 'application/json', ...(selectedWorkspace ? { 'X-ApexVoid-Workspace': selectedWorkspace } : {}), ...(options.body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  })
  if (!response.ok) {
    const errorBody = await response.json().catch(() => null) as { error?: { code?: string } } | null
    const code = errorBody?.error?.code ?? 'REQUEST_FAILED'
    throw new Error(response.status === 409 ? 'That time is no longer available.' : response.status === 403 ? 'You do not have permission for this operation.' : response.status === 503 ? 'The Photobooth schema upgrade or Enterprise authorization is still pending.' : `${code} (HTTP ${response.status})`)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

const appTabs: { key: Tab; text: string; icon: typeof Coffee }[] = [
  { key: 'overview', text: 'Overview', icon: LayoutDashboard },
  { key: 'bookings', text: 'Bookings', icon: CalendarDays },
  { key: 'history', text: 'Booking history', icon: History },
  { key: 'counter', text: 'Photobooth counter', icon: Coffee },
  { key: 'setup', text: 'Operations setup', icon: Settings2 },
]

function Pane({ title, children, extra }: { title: string; children: ReactNode; extra?: ReactNode }) { return <section className="pane"><header className="pane-head"><h2>{title}</h2>{extra}</header><div className="pane-body">{children}</div></section> }
function Pill({ value }: { value: string }) { return <span className={`pill pill-${value}`}>{statusLabel(value)}</span> }
function Banner({ message, onClose }: { message: string | null; onClose: () => void }) { return message && <div role="alert" className="banner"><span>{message}</span><button aria-label="Dismiss message" onClick={onClose}><X size={15} /></button></div> }

export function App() {
  const [appContext, setAppContext] = useState<AppContext>(fallbackAppContext)
  const [tab, setTab] = useState<Tab>('overview')
  const [dark, setDark] = useState(() => localStorage.getItem('apexvoid.photobooth.theme') !== 'light')
  const [items, setItems] = useState<Item[]>([])
  const [orders, setOrders] = useState<Order[]>([])
  const [booths, setBooths] = useState<Booth[]>([])
  const [bookings, setBookings] = useState<Booking[]>([])
  const [historyPage, setHistoryPage] = useState(1)
  const [bookingHistory, setBookingHistory] = useState<Page<Booking>>({ items: [], page: 1, page_size: 50, total: 0, has_more: false })
  const [schedules, setSchedules] = useState<Schedule[]>([])
  const [blackouts, setBlackouts] = useState<Blackout[]>([])
  const [selectedDate, setSelectedDate] = useState(() => new Date())
  const [calendarMode, setCalendarMode] = useState<'day' | 'week'>('day')
  const [bookingFilter, setBookingFilter] = useState({ booth: '', status: '', guest: '' })
  const [selectedBooking, setSelectedBooking] = useState<Booking | null>(null)
  const [events, setEvents] = useState<BookingEvent[]>([])
  const [slots, setSlots] = useState<{ start: string; end: string }[]>([])
  const [availabilityLoading, setAvailabilityLoading] = useState(false)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [newItem, setNewItem] = useState({ name: '', sku: '', kind: 'drink' as Item['kind'], price_vnd: '', duration_minutes: '20' })
  const [boothName, setBoothName] = useState('')
  const [reservation, setReservation] = useState({ booth_id: '', package_id: '', guest_name: '', guest_phone: '', party_size: '1', slot: '', notes: '' })
  const [cart, setCart] = useState<Record<string, number>>({})
  const [orderNote, setOrderNote] = useState('')
  const [blackout, setBlackout] = useState({ booth_id: '', start: '', end: '', reason: '' })

  const drinks = useMemo(() => items.filter(item => item.active && item.kind === 'drink'), [items])
  const photos = useMemo(() => items.filter(item => item.active && item.kind === 'photo'), [items])
  const cartItems = useMemo(() => drinks.filter(item => cart[item.id] > 0), [drinks, cart])
  const cartTotal = cartItems.reduce((sum, item) => sum + item.price_vnd * cart[item.id], 0)
  const activeBookings = bookings.filter(item => ['confirmed', 'checked_in', 'in_progress'].includes(item.status))
  const days = useMemo(() => { const start = new Date(selectedDate); start.setDate(start.getDate() - ((start.getDay() + 6) % 7)); return Array.from({ length: 7 }, (_, index) => { const day = new Date(start); day.setDate(start.getDate() + index); return day }) }, [selectedDate])

  const fetchCalendarBookings = useCallback(async () => {
    const monday = new Date(selectedDate)
    monday.setDate(monday.getDate() - ((monday.getDay() + 6) % 7))
    const nextMonday = new Date(monday)
    nextMonday.setDate(monday.getDate() + 7)
    const from = vietnamLocalToISO(`${dateKey(monday)}T00:00`)
    const to = vietnamLocalToISO(`${dateKey(nextMonday)}T00:00`)
    const all: Booking[] = []
    for (let page = 1; page <= 100; page++) {
      const result = await api<Page<Booking>>(`/bookings?page=${page}&page_size=200&from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`)
      all.push(...result.items)
      if (!result.has_more) return all
    }
    throw new Error('Booking calendar contains too many results. Narrow the date range.')
  }, [selectedDate])

  const loadContext = useCallback(async () => { try { const context = await api<AppContext>('/context'); if (context.application_id === 'photobooth' && context.display_name.trim()) setAppContext({ ...context, display_name: context.display_name.trim() }) } catch { /* Enterprise metadata is optional for the shell. */ } }, [])
  const load = useCallback(async () => {
    setLoading(true)
    const jobs = await Promise.allSettled([api<Item[]>('/menu'), api<Order[]>('/orders'), api<Booth[]>('/booths'), fetchCalendarBookings(), api<Page<Booking>>(`/bookings?page=${historyPage}&page_size=50`)])
    const failed: string[] = []
    if (jobs[0].status === 'fulfilled') setItems(jobs[0].value as Item[]); else failed.push('menu')
    if (jobs[1].status === 'fulfilled') setOrders(jobs[1].value as Order[]); else failed.push('orders')
    if (jobs[2].status === 'fulfilled') setBooths(jobs[2].value as Booth[]); else failed.push('booths')
    if (jobs[3].status === 'fulfilled') setBookings(jobs[3].value as Booking[]); else failed.push('booking calendar')
    if (jobs[4].status === 'fulfilled') setBookingHistory(jobs[4].value as Page<Booking>); else failed.push('booking history')
    setError(failed.length ? `Unable to load ${failed.join(', ')}. Check workspace permissions or migration readiness.` : null)
    setLoading(false)
  }, [fetchCalendarBookings, historyPage])
  const loadOperations = useCallback(async () => { const results = await Promise.allSettled([api<Schedule[]>('/schedules'), api<Blackout[]>('/blackouts')]); if (results[0].status === 'fulfilled') setSchedules(results[0].value); if (results[1].status === 'fulfilled') setBlackouts(results[1].value) }, [])
  const loadEvents = useCallback(async (booking: Booking) => { setSelectedBooking(booking); try { setEvents(await api<BookingEvent[]>(`/bookings/${booking.id}/events`)) } catch { setEvents([]) } }, [])

  useEffect(() => { void loadContext(); void load(); void loadOperations() }, [loadContext, load, loadOperations])
  useEffect(() => { document.title = appContext.display_name; document.documentElement.dataset.theme = dark ? 'dark' : 'light'; localStorage.setItem('apexvoid.photobooth.theme', dark ? 'dark' : 'light') }, [appContext.display_name, dark])
  useEffect(() => { if (!notice) return; const timeout = window.setTimeout(() => setNotice(null), 4500); return () => window.clearTimeout(timeout) }, [notice])
  useEffect(() => { if (!reservation.booth_id || !reservation.package_id) { setSlots([]); setAvailabilityLoading(false); return }; const date = new Date(selectedDate); date.setHours(12, 0, 0, 0); setAvailabilityLoading(true); void api<{ slots: { start: string; end: string }[] }>(`/bookings/availability?booth_id=${encodeURIComponent(reservation.booth_id)}&package_id=${encodeURIComponent(reservation.package_id)}&date=${encodeURIComponent(date.toISOString())}`).then(value => setSlots(value.slots)).catch(() => setSlots([])).finally(() => setAvailabilityLoading(false)) }, [reservation.booth_id, reservation.package_id, selectedDate])

  async function mutate<T>(job: () => Promise<T>, success: string, after?: () => void) { setBusy(true); setError(null); setNotice(null); try { await job(); setNotice(success); after?.(); await load(); await loadOperations() } catch (cause) { setError(cause instanceof Error ? cause.message : 'Request failed') } finally { setBusy(false) } }
  function navigate(next: Tab) { setNotice(null); setError(null); setTab(next) }
  function makeOrder(event: FormEvent) { event.preventDefault(); if (!cartItems.length) return; void mutate(() => api('/orders', { body: { note: orderNote, lines: cartItems.map(item => ({ item_id: item.id, quantity: cart[item.id] })) } }), 'Order opened.', () => { setCart({}); setOrderNote('') }) }
  function addItem(event: FormEvent) { event.preventDefault(); const price = Number(newItem.price_vnd); const duration = Number(newItem.duration_minutes); if (!Number.isSafeInteger(price) || price < 0 || !Number.isInteger(duration) || duration < 5 || duration > 480) { setError('Enter a valid price and duration.'); return } void mutate(() => api('/menu', { body: { ...newItem, price_vnd: price, duration_minutes: duration } }), 'Catalog item created.', () => setNewItem({ name: '', sku: '', kind: 'drink', price_vnd: '', duration_minutes: '20' })) }
  function book(event: FormEvent, after?: () => void) { event.preventDefault(); const slot = slots.find(item => item.start === reservation.slot); if (!slot) { setError('Choose a backend-confirmed available slot.'); return } void mutate(() => api('/bookings', { body: { booth_id: reservation.booth_id, package_id: reservation.package_id, guest_name: reservation.guest_name, guest_phone: reservation.guest_phone, party_size: Number(reservation.party_size), notes: reservation.notes, start: slot.start, end: slot.end, idempotency_key: crypto.randomUUID() } }), 'Reservation confirmed.', () => { setReservation(current => ({ ...current, guest_name: '', guest_phone: '', notes: '', slot: '' })); after?.() }) }
  function transition(booking: Booking, action: string, reason = '') { void mutate(() => api(`/bookings/${booking.id}/${action}`, { body: { reason } }), `${action === 'check-in' ? 'Guest checked in.' : action === 'start' ? 'Session started.' : action === 'complete' ? 'Session completed.' : action === 'no-show' ? 'Marked as no-show.' : 'Booking cancelled.'}`) }
  function reschedule(booking: Booking, startValue: string) { const start = new Date(vietnamLocalToISO(startValue)); if (!Number.isFinite(start.getTime())) { setError('Choose a valid reschedule time.'); return }; const end = new Date(start.getTime() + (new Date(booking.end).getTime() - new Date(booking.start).getTime())); void mutate(() => api(`/bookings/${booking.id}/reschedule`, { body: { booth_id: booking.booth_id, package_id: booking.package_id, guest_name: booking.guest_name, guest_phone: booking.guest_phone ?? '', guest_email: booking.guest_email ?? '', party_size: booking.party_size, notes: booking.notes ?? '', start: start.toISOString(), end: end.toISOString() } }), 'Booking rescheduled.') }
  const visibleBookings = bookings.filter(item => (!bookingFilter.booth || item.booth_id === bookingFilter.booth) && (!bookingFilter.status || item.status === bookingFilter.status) && (!bookingFilter.guest || item.guest_name.toLowerCase().includes(bookingFilter.guest.toLowerCase())))
  const dayBookings = visibleBookings.filter(item => { const key = dateKey(selectedDate); return dateKey(new Date(item.start)) === key || dateKey(new Date(item.end)) === key })

  return <div className="app-shell"><aside className="sidebar"><div className="brand"><div className="brand-icon"><Coffee size={23} /></div><div><b>{appContext.display_name}</b><small>PHOTOBOOTH OPS</small></div></div><div className="side-label">WORKSPACE</div><nav aria-label="Photobooth navigation">{appTabs.map(item => <button key={item.key} onClick={() => navigate(item.key)} aria-current={tab === item.key ? 'page' : undefined} className={tab === item.key ? 'nav-active' : ''}><item.icon size={18} />{item.text}</button>)}</nav><div className="side-spacer" /><div className="side-bottom"><div className="status-dot" />Connected via ApexVoid Enterprise</div></aside><main className="main"><header className="topbar"><div><div className="crumb">Business applications <span>/</span> {appContext.display_name}</div><h1>{appTabs.find(item => item.key === tab)?.text}</h1></div><div className="top-actions"><button className="icon-btn" title="Refresh" aria-label="Refresh data" onClick={() => void load()}><RefreshCw size={18} /></button><button className="icon-btn" title="Toggle color mode" aria-label="Toggle color mode" onClick={() => setDark(current => !current)}>{dark ? <Sun size={18} /> : <Moon size={18} />}</button></div></header><div className="content"><Banner message={error} onClose={() => setError(null)} />{notice && <div className="notice">{notice}</div>}{loading && <div className="loading">Synchronizing Photobooth workspace…</div>}
        {tab === 'overview' && <><div className="intro"><div className="eyebrow">PHOTOBOOTH OPERATIONS</div><h2>Great sessions. Lasting memories.</h2><p>Coordinate photo-booth reservations, guest flow, and the optional café counter from one workspace-aware application.</p><button className="primary intro-button" onClick={() => navigate('bookings')}><CalendarDays size={16} />Open booking calendar</button></div><div className="stats"><div><span>Today's bookings</span><strong>{bookings.filter(item => dateKey(new Date(item.start)) === dateKey(new Date())).length}</strong><small>All booking states</small><CalendarDays size={21} /></div><div><span>Checked in</span><strong>{bookings.filter(item => item.status === 'checked_in').length}</strong><small>Guests on site</small><UserRound size={21} /></div><div><span>Sessions live</span><strong>{bookings.filter(item => item.status === 'in_progress').length}</strong><small>Booths in use</small><Camera size={21} /></div><div><span>Completed</span><strong>{bookings.filter(item => item.status === 'completed').length}</strong><small>Loaded booking history</small><Check size={21} /></div></div><div className="columns"><Pane title="Next reservations" extra={<button className="text-action" onClick={() => navigate('bookings')}>Open calendar →</button>}>{activeBookings.slice(0, 6).map(item => <BookingRow key={item.id} booking={item} onClick={() => void loadEvents(item)} />)}{!activeBookings.length && <p className="empty">No active reservations. The calendar is ready for the next guest.</p>}</Pane><Pane title="Booth utilization"><div className="utilization-list">{booths.map(booth => { const count = activeBookings.filter(item => item.booth_id === booth.id).length; return <div className="utilization" key={booth.id}><div><b>{booth.name}</b><small>{count ? `${count} active session${count > 1 ? 's' : ''}` : 'Available today'}</small></div><div className="progress"><i style={{ width: `${Math.min(100, count * 38)}%` }} /></div></div> })}{!booths.length && <p className="empty">Add a booth in Operations setup.</p>}</div></Pane></div></>}
        {tab === 'bookings' && <BookingWorkspace booths={booths} photos={photos} bookings={visibleBookings} dayBookings={dayBookings} days={days} selectedDate={selectedDate} setSelectedDate={setSelectedDate} calendarMode={calendarMode} setCalendarMode={setCalendarMode} bookingFilter={bookingFilter} setBookingFilter={setBookingFilter} reservation={reservation} setReservation={setReservation} slots={slots} book={book} availabilityLoading={availabilityLoading} busy={busy} loadEvents={loadEvents} transition={transition} />}
        {tab === 'history' && <HistoryView bookings={bookingHistory.items} page={bookingHistory.page} total={bookingHistory.total} hasMore={bookingHistory.has_more} onPageChange={setHistoryPage} selectedBooking={selectedBooking} events={events} loadEvents={loadEvents} />}
        {tab === 'counter' && <CounterView drinks={drinks} orders={orders} cart={cart} setCart={setCart} cartItems={cartItems} cartTotal={cartTotal} orderNote={orderNote} setOrderNote={setOrderNote} makeOrder={makeOrder} busy={busy} transitionOrder={(id, action) => void mutate(() => api(`/orders/${id}/${action}`, { body: {} }), `Order ${action}d.`)} />}
        {tab === 'setup' && <SetupView items={items} newItem={newItem} setNewItem={setNewItem} addItem={addItem} booths={booths} boothName={boothName} setBoothName={setBoothName} addBooth={() => void mutate(() => api('/booths', { body: { name: boothName } }), 'Booth created.', () => setBoothName(''))} schedules={schedules} saveSchedule={(schedule) => void mutate(() => api(`/schedules/${schedule.weekday}`, { method: 'PUT', body: schedule }), 'Schedule saved.', loadOperations)} blackouts={blackouts} blackout={blackout} setBlackout={setBlackout} addBlackout={() => void mutate(() => api('/blackouts', { body: blackout }), 'Blackout added.', () => setBlackout({ booth_id: '', start: '', end: '', reason: '' }))} deleteBlackout={(id) => void mutate(() => api(`/blackouts/${id}`, { method: 'DELETE' }), 'Blackout removed.', loadOperations)} busy={busy} />}
      </div><footer><span>{appContext.display_name} · Workspace-isolated application</span><span><TicketCheck size={13} /> Booking times are checked by the backend</span></footer></main>{selectedBooking && <BookingDrawer booking={selectedBooking} events={events} onClose={() => setSelectedBooking(null)} transition={transition} reschedule={reschedule} />}</div>
}

function BookingRow({ booking, onClick }: { booking: Booking; onClick: () => void }) { return <button className="record record-button" onClick={onClick}><div><b>{booking.booking_ref || `#${booking.id.slice(0, 8)}`} · {booking.guest_name}</b><small>{booking.booth_name} · {timeText(booking.start)} · {booking.package_name}</small></div><Pill value={booking.status} /></button> }


type BookingWorkspaceProps = { booths: Booth[]; photos: Item[]; bookings: Booking[]; dayBookings: Booking[]; days: Date[]; selectedDate: Date; setSelectedDate: (value: Date) => void; calendarMode: 'day' | 'week'; setCalendarMode: (value: 'day' | 'week') => void; bookingFilter: { booth: string; status: string; guest: string }; setBookingFilter: (value: { booth: string; status: string; guest: string }) => void; reservation: { booth_id: string; package_id: string; guest_name: string; guest_phone: string; party_size: string; slot: string; notes: string }; setReservation: (value: any) => void; slots: { start: string; end: string }[]; availabilityLoading: boolean; book: (event: FormEvent, after?: () => void) => void; busy: boolean; loadEvents: (booking: Booking) => void; transition: (booking: Booking, action: string, reason?: string) => void }

function BookingWorkspace(props: BookingWorkspaceProps) {
  const { booths, photos, bookings, dayBookings, days, selectedDate, setSelectedDate, calendarMode, setCalendarMode, bookingFilter, setBookingFilter, reservation, setReservation, slots, availabilityLoading, book, busy, loadEvents, transition } = props
  const [createOpen, setCreateOpen] = useState(false)
  const calendarDatePickerRef = useRef<HTMLInputElement>(null)
  const closeCreate = useCallback(() => setCreateOpen(false), [])
  const shift = (amount: number) => { const next = new Date(selectedDate); next.setDate(next.getDate() + amount); setSelectedDate(next) }
  const openCalendarDatePicker = () => {
    const picker = calendarDatePickerRef.current as (HTMLInputElement & { showPicker?: () => void }) | null
    if (!picker) return
    if (picker.showPicker) picker.showPicker()
    else picker.focus()
  }
  const selectCalendarDate = (value: string) => {
    const next = new Date(`${value}T12:00:00`)
    if (Number.isFinite(next.getTime())) setSelectedDate(next)
  }
  const bookingsForDay = (day: Date) => bookings.filter(item => dateKey(new Date(item.start)) === dateKey(day)).sort((a, b) => a.start.localeCompare(b.start))

  return <>
    <div className="booking-command">
      <div>
        <div className="eyebrow">RESERVATION DESK</div>
        <h2>Keep every session moving.</h2>
        <p>Plan the floor, find a guest, and create a confirmed session when the right moment is available.</p>
      </div>
      <div className="booking-command-actions">
        <div className="booking-snapshot"><CalendarDays size={17} /><span><small>Showing today</small><b>{dayBookings.length} reservation{dayBookings.length === 1 ? '' : 's'}</b></span></div>
        <button className="primary booking-create-button" onClick={() => setCreateOpen(true)}><Plus size={17} />New reservation</button>
      </div>
    </div>
    <div className="booking-layout">
      <div className="booking-main">
        <Pane title="Booking calendar" extra={<div className="calendar-header-actions"><button className="today-button calendar-today-button" onClick={() => setSelectedDate(new Date())}>Today</button><div className="calendar-actions"><button className={calendarMode === 'day' ? 'seg-active' : ''} onClick={() => setCalendarMode('day')}>Day</button><button className={calendarMode === 'week' ? 'seg-active' : ''} onClick={() => setCalendarMode('week')}>Week</button></div></div>}>
          <div className="calendar-toolbar"><button className="icon-btn" onClick={() => shift(calendarMode === 'week' ? -7 : -1)} aria-label="Previous date"><ChevronLeft size={17} /></button><div className="calendar-date-context"><span>{calendarMode === 'week' ? 'Week view' : 'Selected date'}</span><button type="button" className="calendar-date-trigger" onClick={openCalendarDatePicker} aria-label="Choose calendar date"><b>{calendarMode === 'day' ? selectedDate.toLocaleDateString('vi-VN', { weekday: 'long', day: 'numeric', month: 'long' }) : `${dateLabel(days[0])} – ${dateLabel(days[6])}`}</b></button><input ref={calendarDatePickerRef} className="calendar-date-picker" type="date" value={dateKey(selectedDate)} aria-label="Choose calendar date" onChange={event => selectCalendarDate(event.target.value)} /></div><button className="icon-btn" onClick={() => shift(calendarMode === 'week' ? 7 : 1)} aria-label="Next date"><ChevronRight size={17} /></button></div>
          <div className="calendar-legend" aria-label="Booking status legend"><span><i className="legend-dot confirmed" />Confirmed</span><span><i className="legend-dot checked" />Checked in</span><span><i className="legend-dot live" />In progress</span></div>
          <div className={calendarMode === 'week' ? 'week-grid' : 'day-grid'}>{(calendarMode === 'week' ? days : [selectedDate]).map(day => { const dayItems = bookingsForDay(day); return <div className="calendar-day" key={dateKey(day)}><div className="calendar-day-head"><span>{dateLabel(day)}</span><b>{dayItems.length}</b></div>{dayItems.map(item => <button key={item.id} className={`calendar-card status-${item.status}`} onClick={() => loadEvents(item)}><span>{new Date(item.start).toLocaleTimeString('vi-VN', { hour: '2-digit', minute: '2-digit' })}–{new Date(item.end).toLocaleTimeString('vi-VN', { hour: '2-digit', minute: '2-digit' })}</span><b>{item.guest_name}</b><small>{item.booth_name} · {item.package_name}</small><Pill value={item.status} /></button>)}{!dayItems.length && <div className="calendar-empty"><div className="empty-calendar-icon"><CalendarDays size={20} /></div><b>Floor is open</b><p>No reservations are scheduled for this day.</p><button type="button" onClick={() => setCreateOpen(true)}><Plus size={14} />Schedule a session</button></div>}</div> })}</div>
        </Pane>
        <Pane title="Upcoming and recent activity" extra={<span className="subtle">{dayBookings.length} for this date</span>}>
          {dayBookings.map(item => <div className="booking-line" key={item.id}><BookingRow booking={item} onClick={() => loadEvents(item)} /><div className="row-actions">{item.status === 'confirmed' && <button disabled={busy} onClick={() => transition(item, 'check-in')}>Check in</button>}{item.status === 'checked_in' && <button disabled={busy} onClick={() => transition(item, 'start')}>Start session</button>}{item.status === 'in_progress' && <button disabled={busy} onClick={() => transition(item, 'complete')}>Complete</button>}{item.status === 'confirmed' && <button disabled={busy} onClick={() => transition(item, 'no-show', 'Guest did not arrive')}>No-show</button>}{['confirmed', 'checked_in'].includes(item.status) && <button disabled={busy} onClick={() => transition(item, 'cancel', 'Cancelled by staff')}>Cancel</button>}</div></div>)}{!dayBookings.length && <p className="empty">No reservations match this date and filter.</p>}
        </Pane>
      </div>
      <aside className="booking-side">
        <Pane title="Find a booking" extra={<Search size={16} className="pane-icon" />}>
          <div className="search-intro">Search by guest, then narrow the list by station or session status.</div>
          <div className="filter-stack"><label className="search-field"><Search size={15} /><input aria-label="Search guest" placeholder="Search guest name" value={bookingFilter.guest} onChange={event => setBookingFilter({ ...bookingFilter, guest: event.target.value })} /></label><select aria-label="Filter by booth" value={bookingFilter.booth} onChange={event => setBookingFilter({ ...bookingFilter, booth: event.target.value })}><option value="">All booths</option>{booths.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select><select aria-label="Filter by status" value={bookingFilter.status} onChange={event => setBookingFilter({ ...bookingFilter, status: event.target.value })}><option value="">All statuses</option>{['confirmed', 'checked_in', 'in_progress', 'completed', 'cancelled', 'no_show'].map(item => <option key={item} value={item}>{statusLabel(item)}</option>)}</select></div>
        </Pane>
        <div className="booking-tip"><div className="tip-icon"><Clock3 size={17} /></div><div><b>Availability is live</b><p>Every slot is calculated by the backend against schedules, holds, blackouts, and existing sessions.</p></div></div>
      </aside>
    </div>
    {createOpen && <BookingCreateModal booths={booths} photos={photos} selectedDate={selectedDate} setSelectedDate={setSelectedDate} reservation={reservation} setReservation={setReservation} slots={slots} availabilityLoading={availabilityLoading} busy={busy} book={book} onClose={closeCreate} />}
  </>
}

function BookingCreateModal({ booths, photos, selectedDate, setSelectedDate, reservation, setReservation, slots, availabilityLoading, busy, book, onClose }: { booths: Booth[]; photos: Item[]; selectedDate: Date; setSelectedDate: (value: Date) => void; reservation: BookingWorkspaceProps['reservation']; setReservation: BookingWorkspaceProps['setReservation']; slots: BookingWorkspaceProps['slots']; availabilityLoading: boolean; busy: boolean; book: BookingWorkspaceProps['book']; onClose: () => void }) {
  const dialogRef = useRef<HTMLElement>(null)
  useEffect(() => {
    const previousFocus = document.activeElement as HTMLElement | null
    document.body.style.overflow = 'hidden'
    dialogRef.current?.querySelector<HTMLElement>('[data-modal-autofocus]')?.focus()
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); onClose(); return }
      if (event.key !== 'Tab' || !dialogRef.current) return
      const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled])'))
      if (!focusable.length) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => { document.body.style.overflow = ''; document.removeEventListener('keydown', handleKeyDown); previousFocus?.focus() }
  }, [onClose])
  const setField = (field: keyof typeof reservation, value: string) => setReservation({ ...reservation, [field]: value })
  const dateValue = dateKey(selectedDate)
  const ready = Boolean(reservation.booth_id && reservation.package_id && reservation.slot && reservation.guest_name.trim() && slots.length)
  const availabilityMessage = availabilityLoading ? 'Checking live availability…' : !reservation.booth_id || !reservation.package_id ? 'Choose a booth and package to see confirmed time slots.' : slots.length ? `${slots.length} live slot${slots.length === 1 ? '' : 's'} available for this date.` : 'No slots available. Try another date or package.'
  return <div className="modal-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}><section ref={dialogRef} className="booking-modal" role="dialog" aria-modal="true" aria-labelledby="booking-modal-title" aria-describedby="booking-modal-description">
    <header className="modal-head"><div><span className="eyebrow">NEW RESERVATION</span><h2 id="booking-modal-title">Create a confirmed session</h2><p id="booking-modal-description">Choose a live slot, then add the guest details your team needs.</p></div><button className="icon-btn" type="button" onClick={onClose} aria-label="Close new reservation"><X size={18} /></button></header>
    <form className="modal-form" onSubmit={event => book(event, onClose)}>
      <div className="form-section"><div className="form-section-head"><span className="section-number">01</span><div><h3>Reservation details</h3><p>We’ll only show slots the backend can confirm.</p></div></div><div className="field-grid two"><label>Booth<select required value={reservation.booth_id} onChange={event => setField('booth_id', event.target.value)}><option value="">Select booth</option>{booths.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><label>Service package<select required value={reservation.package_id} onChange={event => setField('package_id', event.target.value)}><option value="">Select package</option>{photos.map(item => <option key={item.id} value={item.id}>{item.name} · {item.duration_minutes} min</option>)}</select></label><label>Session date<input className="date-input" required type="date" value={dateValue} onClick={event => { const input = event.currentTarget as HTMLInputElement & { showPicker?: () => void }; input.showPicker?.() }} onChange={event => { const next = new Date(`${event.target.value}T12:00:00`); setSelectedDate(next); setField('slot', '') }} /></label><label>Party size<input required type="number" min="1" max="100" value={reservation.party_size} onChange={event => setField('party_size', event.target.value)} /></label></div><div className="slot-area"><div className="slot-label"><span>Available time</span><small>{availabilityMessage}</small></div>{availabilityLoading ? <div className="slot-loading"><RefreshCw size={15} />Refreshing live slots</div> : slots.length ? <div className="slot-grid" role="group" aria-label="Available time slots">{slots.map(slot => <button type="button" key={slot.start} className={reservation.slot === slot.start ? 'slot-button slot-selected' : 'slot-button'} aria-pressed={reservation.slot === slot.start} onClick={() => setField('slot', slot.start)}>{new Date(slot.start).toLocaleTimeString('vi-VN', { hour: '2-digit', minute: '2-digit' })}<small>{new Date(slot.end).toLocaleTimeString('vi-VN', { hour: '2-digit', minute: '2-digit' })}</small></button>)}</div> : <div className="slot-empty"><Clock3 size={16} />No confirmed times yet</div>}</div></div>
      <div className="form-section"><div className="form-section-head"><span className="section-number">02</span><div><h3>Guest details</h3><p>Keep the confirmation easy to find later.</p></div></div><div className="field-grid two"><label>Guest name<input required maxLength={120} data-modal-autofocus value={reservation.guest_name} onChange={event => setField('guest_name', event.target.value)} placeholder="Customer or group name" /></label><label>Contact phone<input maxLength={40} value={reservation.guest_phone} onChange={event => setField('guest_phone', event.target.value)} placeholder="Optional" /></label></div></div>
      <div className="form-section"><div className="form-section-head"><span className="section-number">03</span><div><h3>Additional notes</h3><p>Share preparation details with the floor team.</p></div></div><label>Notes<textarea rows={3} maxLength={1000} value={reservation.notes} onChange={event => setField('notes', event.target.value)} placeholder="Preparation notes, preferences, or arrival context" /></label></div>
      <footer className="modal-footer"><span><TicketCheck size={15} />A booking is created only after the server confirms this slot.</span><div><button className="secondary-button" type="button" onClick={onClose}>Cancel</button><button className="primary" type="submit" disabled={busy || !ready}>{busy ? <><RefreshCw size={16} className="spin" />Confirming…</> : <><TicketCheck size={16} />Confirm reservation</>}</button></div></footer>
    </form>
  </section></div>
}

function HistoryView({ bookings, page, total, hasMore, onPageChange, selectedBooking, events, loadEvents }: { bookings: Booking[]; page: number; total: number; hasMore: boolean; onPageChange: (value: number) => void; selectedBooking: Booking | null; events: BookingEvent[]; loadEvents: (booking: Booking) => void }) { return <div className="history-layout"><Pane title="Booking history" extra={<span className="subtle">{total} records · page {page}</span>}><div className="history-table"><div className="history-head"><span>Reference</span><span>Guest</span><span>Session</span><span>Status</span></div>{bookings.map(item => <button className="history-row" key={item.id} onClick={() => loadEvents(item)}><b>{item.booking_ref || item.id.slice(0, 8)}</b><span>{item.guest_name}<small>{item.guest_phone || 'No contact'}</small></span><span>{timeText(item.start)}<small>{item.booth_name}</small></span><Pill value={item.status} /></button>)}{!bookings.length && <p className="empty">No booking history yet.</p>}</div><div className="calendar-actions" aria-label="Booking history pages"><button disabled={page <= 1} onClick={() => onPageChange(Math.max(1, page - 1))}>Previous</button><span>Page {page}</span><button disabled={!hasMore} onClick={() => onPageChange(page + 1)}>Next</button></div></Pane><Pane title={selectedBooking ? `${selectedBooking.booking_ref} activity` : 'Select a booking'}>{selectedBooking ? <div className="timeline">{events.map(event => <div className="timeline-item" key={event.id}><i /><div><b>{statusLabel(event.event_type)}</b><small>{timeText(event.created_at)} · {event.actor_id.slice(0, 8)}</small>{event.reason && <p>{event.reason}</p>}</div></div>)}{!events.length && <p className="empty">No events recorded.</p>}</div> : <p className="empty">Click a booking to inspect its immutable activity timeline.</p>}</Pane></div> }

function BookingDrawer({ booking, events, onClose, transition, reschedule }: { booking: Booking; events: BookingEvent[]; onClose: () => void; transition: (booking: Booking, action: string, reason?: string) => void; reschedule: (booking: Booking, start: string) => void }) { const [start, setStart] = useState(() => bookingDateTimeInput(booking.start)); return <div className="drawer-backdrop" onClick={onClose}><aside className="drawer" onClick={event => event.stopPropagation()}><div className="drawer-head"><div><span className="eyebrow">{booking.booking_ref}</span><h2>{booking.guest_name}</h2><small>{booking.booth_name} · {booking.package_name}</small></div><button className="icon-btn" onClick={onClose} aria-label="Close booking details"><X size={17} /></button></div><div className="drawer-body"><div className="detail-grid"><div><small>Session</small><b>{timeText(booking.start)}</b></div><div><small>Party</small><b>{booking.party_size} guest{booking.party_size === 1 ? '' : 's'}</b></div><div><small>Contact</small><b>{booking.guest_phone || 'Not provided'}</b></div><div><small>Status</small><Pill value={booking.status} /></div></div>{booking.notes && <p className="detail-note">{booking.notes}</p>}<div className="drawer-actions">{booking.status === 'confirmed' && <button onClick={() => transition(booking, 'check-in')}>Check in</button>}{booking.status === 'checked_in' && <button onClick={() => transition(booking, 'start')}>Start session</button>}{booking.status === 'in_progress' && <button onClick={() => transition(booking, 'complete')}>Complete</button>}{['confirmed', 'checked_in'].includes(booking.status) && <button onClick={() => transition(booking, 'cancel', 'Cancelled by staff')}>Cancel</button>}</div>{booking.status === 'confirmed' && <form className="reschedule-form" onSubmit={event => { event.preventDefault(); reschedule(booking, start) }}><label>Reschedule session<input type="datetime-local" value={start} onChange={event => setStart(event.target.value)} /></label><button className="primary" type="submit"><CalendarDays size={15} />Save new time</button></form>}<h3>Activity timeline</h3><div className="timeline">{events.map(event => <div className="timeline-item" key={event.id}><i /><div><b>{statusLabel(event.event_type)}</b><small>{timeText(event.created_at)}</small></div></div>)}</div></div></aside></div> }

function CounterView({ drinks, orders, cart, setCart, cartItems, cartTotal, orderNote, setOrderNote, makeOrder, busy, transitionOrder }: { drinks: Item[]; orders: Order[]; cart: Record<string, number>; setCart: (value: Record<string, number>) => void; cartItems: Item[]; cartTotal: number; orderNote: string; setOrderNote: (value: string) => void; makeOrder: (event: FormEvent) => void; busy: boolean; transitionOrder: (id: string, action: string) => void }) { return <div className="counter-grid"><Pane title="Photobooth menu" extra={<span className="subtle">Prices in VND</span>}><div className="items-grid">{drinks.map(item => <button key={item.id} className="menu-tile selectable" onClick={() => setCart({ ...cart, [item.id]: Math.min(99, (cart[item.id] ?? 0) + 1) })}><div className="tile-icon drink"><Coffee size={22} /></div><b>{item.name}</b><small>{item.sku}</small><strong>{money(item.price_vnd)}</strong><span className="add-hint"><Plus size={13} /> Add</span></button>)}</div>{!drinks.length && <p className="empty">Add drinks in Operations setup to begin.</p>}</Pane><div className="stack"><Pane title="Current cart"><form className="form" onSubmit={makeOrder}>{cartItems.map(item => <div className="cart-line" key={item.id}><div><b>{item.name}</b><small>{money(item.price_vnd)}</small></div><div className="quantity"><button type="button" onClick={() => setCart({ ...cart, [item.id]: Math.max(0, (cart[item.id] ?? 0) - 1) })}>−</button><span>{cart[item.id]}</span><button type="button" onClick={() => setCart({ ...cart, [item.id]: Math.min(99, (cart[item.id] ?? 0) + 1) })}>+</button></div></div>)}{!cartItems.length && <p className="empty">Pick a drink to start an order.</p>}<div className="total"><span>Order total</span><b>{money(cartTotal)}</b></div><label>Order note<input value={orderNote} maxLength={300} onChange={event => setOrderNote(event.target.value)} placeholder="Table number or takeaway" /></label><button className="primary" disabled={!cartItems.length || busy}><Coffee size={17} />Create order</button></form></Pane><Pane title="Recent orders">{orders.map(order => <div className="record" key={order.id}><div><b>#{order.id.slice(0, 8)} · {money(order.total_vnd)}</b><small>{timeText(order.created_at)}</small></div>{order.status === 'open' ? <div className="row-actions"><button onClick={() => transitionOrder(order.id, 'serve')}>Serve</button><button onClick={() => transitionOrder(order.id, 'cancel')}>Cancel</button></div> : <Pill value={order.status} />}</div>)}</Pane></div></div> }

function SetupView({ items, newItem, setNewItem, addItem, booths, boothName, setBoothName, addBooth, schedules, saveSchedule, blackouts, blackout, setBlackout, addBlackout, deleteBlackout, busy }: { items: Item[]; newItem: any; setNewItem: (value: any) => void; addItem: (event: FormEvent) => void; booths: Booth[]; boothName: string; setBoothName: (value: string) => void; addBooth: () => void; schedules: Schedule[]; saveSchedule: (schedule: Schedule) => void; blackouts: Blackout[]; blackout: any; setBlackout: (value: any) => void; addBlackout: () => void; deleteBlackout: (id: string) => void; busy: boolean }) { const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']; return <div className="setup-grid"><div className="stack"><Pane title="Operating hours"><p className="helper">Schedules use the workspace timezone. Booth-specific rows override the workspace default.</p><div className="schedule-list">{weekdays.map((name, weekday) => { const existing = schedules.find(item => item.weekday === weekday && !item.booth_id) ?? { weekday, open_time: '09:00', close_time: '21:00', closed: false, slot_increment_minutes: 15, min_advance_minutes: 30, max_horizon_days: 90, timezone: 'Asia/Ho_Chi_Minh' } as Schedule; return <div className="schedule-row" key={weekday}><b>{name}</b><input type="time" value={existing.open_time} disabled={existing.closed} onChange={event => saveSchedule({ ...existing, open_time: event.target.value })} /><span>to</span><input type="time" value={existing.close_time} disabled={existing.closed} onChange={event => saveSchedule({ ...existing, close_time: event.target.value })} /><label className="check"><input type="checkbox" checked={existing.closed} onChange={event => saveSchedule({ ...existing, closed: event.target.checked })} /> Closed</label></div> })}</div></Pane><Pane title="Booth blackouts"><div className="blackout-list">{blackouts.map(item => <div className="record" key={item.id}><div><b>{item.reason}</b><small>{timeText(item.start)} → {timeText(item.end)}</small></div><button className="text-action" onClick={() => deleteBlackout(item.id)}>Remove</button></div>)}{!blackouts.length && <p className="empty">No maintenance or closure windows.</p>}</div><div className="form compact-form"><label>Booth<select value={blackout.booth_id} onChange={event => setBlackout({ ...blackout, booth_id: event.target.value })}><option value="">All booths</option>{booths.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><label>Starts<input type="datetime-local" value={blackout.start ? blackout.start.slice(0, 16) : ''} onChange={event => setBlackout({ ...blackout, start: vietnamLocalToISO(event.target.value) })} /></label><label>Ends<input type="datetime-local" value={blackout.end ? blackout.end.slice(0, 16) : ''} onChange={event => setBlackout({ ...blackout, end: vietnamLocalToISO(event.target.value) })} /></label><label>Reason<input value={blackout.reason} onChange={event => setBlackout({ ...blackout, reason: event.target.value })} placeholder="Maintenance" /></label><button className="primary" disabled={busy} onClick={addBlackout}>Add blackout</button></div></Pane></div><div className="stack"><Pane title="Photo booths"><div className="stations">{booths.map(item => <div className="station" key={item.id}><div className="tile-icon photo"><Camera size={21} /></div><div><b>{item.name}</b><small>{item.active ? 'Active station' : 'Inactive'}</small></div></div>)}</div><div className="inline-form"><input value={boothName} maxLength={100} onChange={event => setBoothName(event.target.value)} placeholder="New station name" /><button className="primary" disabled={busy || !boothName.trim()} onClick={addBooth}><Plus size={16} />Add booth</button></div></Pane><Pane title="Catalog"><div className="items-grid">{items.map(item => <div className="menu-tile" key={item.id}><div className={`tile-icon ${item.kind === 'photo' ? 'photo' : 'drink'}`}>{item.kind === 'photo' ? <Camera size={22} /> : <Coffee size={22} />}</div><b>{item.name}</b><small>{item.kind} · {item.duration_minutes} min</small><strong>{money(item.price_vnd)}</strong></div>)}</div></Pane><Pane title="Add catalog item"><form className="form" onSubmit={addItem}><label>Name<input required value={newItem.name} onChange={event => setNewItem({ ...newItem, name: event.target.value })} /></label><label>SKU<input required value={newItem.sku} onChange={event => setNewItem({ ...newItem, sku: event.target.value })} /></label><label>Type<select value={newItem.kind} onChange={event => setNewItem({ ...newItem, kind: event.target.value })}><option value="drink">Drink</option><option value="photo">Photo package</option></select></label><label>Price (VND)<input required type="number" min="0" value={newItem.price_vnd} onChange={event => setNewItem({ ...newItem, price_vnd: event.target.value })} /></label><label>Duration (minutes)<input required type="number" min="5" max="480" value={newItem.duration_minutes} onChange={event => setNewItem({ ...newItem, duration_minutes: event.target.value })} /></label><button className="primary" disabled={busy}><Plus size={17} />Add catalog item</button></form></Pane></div></div> }
