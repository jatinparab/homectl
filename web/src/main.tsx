import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from '@emotion/react'
import { Theme as RadixTheme } from '@radix-ui/themes'
import App from './App'
import { LiveStateProvider } from './api/live'
import { theme, GlobalStyles } from './theme'
import '@radix-ui/themes/styles.css'
import './index.css'

const queryClient = new QueryClient()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ThemeProvider theme={theme}>
      <RadixTheme accentColor="teal" grayColor="sand" radius="small" scaling="100%">
        <QueryClientProvider client={queryClient}>
          <LiveStateProvider>
            <GlobalStyles />
            <App />
          </LiveStateProvider>
        </QueryClientProvider>
      </RadixTheme>
    </ThemeProvider>
  </React.StrictMode>,
)
