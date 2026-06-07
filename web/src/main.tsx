import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from '@emotion/react'
import App from './App'
import { LiveStateProvider } from './api/live'
import { theme, GlobalStyles } from './theme'
import './index.css'

const queryClient = new QueryClient()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ThemeProvider theme={theme}>
      <QueryClientProvider client={queryClient}>
        <LiveStateProvider>
          <GlobalStyles />
          <App />
        </LiveStateProvider>
      </QueryClientProvider>
    </ThemeProvider>
  </React.StrictMode>,
)
