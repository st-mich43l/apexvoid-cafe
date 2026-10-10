import { useEffect, useMemo, useState } from 'react'
import { CalendarClock, ChevronLeft, ChevronRight, Clock3, Phone, Search, ShieldCheck, UserRound, X } from 'lucide-react'
import type { Booking, Page } from './App'

type CareDeskProps = {
  calendarBookings: Booking[]
  searchBookings: (query: string, status: string, page: number) => Promise<Page<Booking>>
  onOpenBooking: (booking: Booking) => void
  onOpenCalendar: () => void
}

const careStatuses = ['confirmed', 'checked_in', 'in_progress', 'completed', 'cancelled', 'no_show'] as const

function vietnamDateKey(value: string | Date) {
  const date = value instanceof Date ? value : new Date(value)
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: 'Asia/Ho_Chi_Minh', year: 'numeric', month: '2-digit', day: '2-digit',
  }).formatToParts(date)
  const get = (type: string) => parts.find(part => part.type === type)?.value ?? ''
  return `${get('year')}-${get('month')}-${get('day')}`
}

const dateTime = (iso: string) => new Date(iso).toLocaleString('vi-VN', {
  timeZone: 'Asia/Ho_Chi_Minh', day: '2-digit', month: '2-digit',
  hour: '2-digit', minute: '2-digit',
})

const readableStatus = (value: string) => ({
  confirmed: 'Expected', checked_in: 'Checked in', in_progress: 'In session',
  completed: 'Completed', cancelled: 'Cancelled', no_show: 'No-show',
}[value] ?? value.replaceAll('_', ' '))

function phoneHref(phone?: string) {
  if (!phone) return null
  const normalized = phone.replace(/[^\d+]/g, '').replace(/(?!^)\+/g, '')
  return /^\+?\d{7,15}$/.test(normalized) ? `tel:${normalized}` : null
}

function GuestRecord({ booking, onOpen }: { booking: Booking; onOpen: (value: Booking) => void }) {
  const phone = phoneHref(booking.guest_phone)
  return <article className="care-record">
    <div className="care-record-main">
      <div className="care-record-identity">
        <span className="care-record-avatar" aria-hidden="true"><UserRound size={18} /></span>
        <div>
          <strong>{booking.guest_name}</strong>
          <small>{booking.booking_ref || booking.id.slice(0, 8)} · {booking.party_size} guest{booking.party_size === 1 ? '' : 's'}</small>
        </div>
      </div>
      <span className={`care-status care-status-${booking.status}`}>{readableStatus(booking.status)}</span>
    </div>
    <div className="care-record-details">
      <span><CalendarClock size={15} />{dateTime(booking.start)}</span>
      <span><Clock3 size={15} />{booking.booth_name} · {booking.package_name}</span>
      {booking.guest_phone && <span><Phone size={15} />{booking.guest_phone}</span>}
    </div>
    <div className="care-record-actions">
      {phone && <a className="care-button care-button-secondary" href={phone} aria-label={`Call ${booking.guest_name}`}><Phone size={15} />Call guest</a>}
      <button className="care-button care-button-primary" type="button" onClick={() => onOpen(booking)}>Open booking <ChevronRight size={15} /></button>
    </div>
  </article>
}

export function GuestCareDesk({ calendarBookings, searchBookings, onOpenBooking, onOpenCalendar }: CareDeskProps) {
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const [results, setResults] = useState<Page<Booking> | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const query = debouncedSearch.trim()
  const searching = query.length >= 2

  useEffect(() => {
    const timeout = window.setTimeout(() => setDebouncedSearch(search), 350)
    return () => window.clearTimeout(timeout)
  }, [search])

  useEffect(() => {
    let current = true
    if (!searching) {
      setResults(null)
      setError(null)
      setLoading(false)
      return () => { current = false }
    }
    setResults(null)
    setLoading(true)
    setError(null)
    void searchBookings(query, status, page)
      .then(value => { if (current) setResults(value) })
      .catch(cause => { if (current) setError(cause instanceof Error ? cause.message : 'Could not find bookings.') })
      .finally(() => { if (current) setLoading(false) })
    return () => { current = false }
  }, [query, status, page, searching, searchBookings])

  const today = vietnamDateKey(new Date())
  const todaysBookings = useMemo(() => calendarBookings
    .filter(item => vietnamDateKey(item.start) === today)
    .sort((a, b) => a.start.localeCompare(b.start)), [calendarBookings, today])
  const toCareFor = todaysBookings.filter(item => ['confirmed', 'checked_in', 'in_progress'].includes(item.status))
  const awaiting = todaysBookings.filter(item => item.status === 'confirmed').length
  const checkedIn = todaysBookings.filter(item => item.status === 'checked_in').length
  const live = todaysBookings.filter(item => item.status === 'in_progress').length
  const visibleToday = toCareFor.filter(item => !status || item.status === status)
  const records = searching ? results?.items ?? [] : visibleToday
  const pendingSearch = search.trim() !== debouncedSearch.trim()

  return <div className="care-desk">
    <header className="care-hero">
      <div>
        <span className="eyebrow">GUEST CARE DESK</span>
        <h2>Every guest, warmly looked after.</h2>
        <p>Find reservations across your workspace, help arriving guests, and handle session changes from one place.</p>
      </div>
      <button type="button" className="primary" onClick={onOpenCalendar}><CalendarClock size={17} />Booking calendar</button>
    </header>

    <div className="care-stats" aria-label="Today's guest care status">
      <div><small>Expected arrivals</small><strong>{awaiting}</strong><span>Confirmed and waiting</span></div>
      <div><small>Checked in</small><strong>{checkedIn}</strong><span>Ready for their session</span></div>
      <div><small>Sessions running</small><strong>{live}</strong><span>Currently in a booth</span></div>
    </div>

    <section className="care-workspace" aria-labelledby="care-search-title">
      <header className="care-section-head">
        <div><h3 id="care-search-title">Find and help a guest</h3><p>Search any reservation by name, phone number, or booking reference — not just this week's calendar.</p></div>
        <span><ShieldCheck size={14} />Workspace protected</span>
      </header>
      <div className="care-search-bar">
        <label className="care-search-field">
          <Search size={19} aria-hidden="true" />
          <input type="search" value={search} maxLength={120} autoComplete="off" aria-label="Find guest by name, phone, or booking reference" placeholder="Guest name, phone, or reservation reference" onChange={e => { setSearch(e.target.value); setPage(1) }} />
          {search && <button type="button" aria-label="Clear guest search" onClick={() => { setSearch(''); setPage(1) }}><X size={16} /></button>}
        </label>
        <label className="care-status-filter">
          <span>Status</span>
          <select value={status} aria-label="Filter guest care by booking status" onChange={e => { setStatus(e.target.value); setPage(1) }}>
            <option value="">All statuses</option>
            {careStatuses.map(item => <option key={item} value={item}>{readableStatus(item)}</option>)}
          </select>
        </label>
      </div>

      <div className="care-result-header">
        <div><h3>{searching ? 'Matching reservations' : "Today's service queue"}</h3><p>{searching ? 'Results include previous and upcoming sessions.' : 'Guests with active sessions scheduled today.'}</p></div>
        <span>{searching ? (results ? `${results.total} found` : 'Searching…') : `${visibleToday.length} guests`}</span>
      </div>

      {search.trim().length === 1 && <p className="care-hint" role="status">Enter at least two characters to search reservation history.</p>}
      {(loading || pendingSearch) && searching && <p className="care-hint" role="status">Searching bookings…</p>}
      {error && <div className="care-error" role="alert">{error}<button type="button" onClick={() => { setPage(1); setDebouncedSearch(search.trim()) }}>Try again</button></div>}
      <div className="care-record-list">
        {!loading && !pendingSearch && !error && records.map(item => <GuestRecord key={item.id} booking={item} onOpen={onOpenBooking} />)}
        {!loading && !pendingSearch && !error && records.length === 0 && <div className="care-empty"><UserRound size={25} /><h4>{searching ? 'No matching reservations' : 'No guests in the queue'}</h4><p>{searching ? 'Check the guest name, phone, or booking reference.' : 'Confirmed arrivals will appear here on their session date.'}</p></div>}
      </div>
      {searching && results && !loading && !pendingSearch && <div className="care-pagination" aria-label="Guest search pages">
        <button type="button" disabled={page <= 1} onClick={() => setPage(n => Math.max(1, n - 1))}><ChevronLeft size={15} />Previous</button>
        <span>Page {results.page} · {results.total} results</span>
        <button type="button" disabled={!results.has_more} onClick={() => setPage(n => n + 1)}>Next<ChevronRight size={15} /></button>
      </div>}
    </section>
  </div>
}
