import React from 'react';
import ReactDOM from 'react-dom/client';
import './index.css';
import App from './App';

document.documentElement.dataset.theme = localStorage.getItem('tracium_theme')
  ?? (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
