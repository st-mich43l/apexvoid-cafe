import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Enterprise strips /apps/photobooth before forwarding to this service.
// The compiled asset URLs must retain the public gateway prefix.
export default defineConfig({ base: '/apps/photobooth/', plugins: [react()] })
