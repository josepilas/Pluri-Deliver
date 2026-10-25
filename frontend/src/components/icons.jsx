// icons.jsx — minimalist stroke SVG icon set (current color).
const S = (props) => ({
  xmlns: 'http://www.w3.org/2000/svg',
  width: props.size || 18,
  height: props.size || 18,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: props.stroke || 1.7,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
})

export const IconLogo = ({ size = 18 }) => (
  <svg {...S({ size })} viewBox="0 0 24 24" fill="none">
    <rect x="4" y="6.5" width="16" height="13" rx="3" stroke="currentColor" strokeWidth="1.7" />
    <path d="M4 10.5h16M12 10.5V19.5" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" />
    <circle cx="7.4" cy="8.5" r="1" fill="currentColor" />
    <path d="M17.5 2.5l3.5 3.5-3.5 3.5" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
    <path d="M21 6h-6" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" />
  </svg>
)

export const IconPackage = ({ size = 18 }) => (
  <svg {...S({ size })}>
    <path d="M21 8l-9-5-9 5v8l9 5 9-5V8z" />
    <path d="M3 8l9 5 9-5M12 13v8" />
  </svg>
)

export const IconBell = ({ size = 18 }) => (
  <svg {...S({ size })}>
    <path d="M18 8a6 6 0 10-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
    <path d="M13.7 21a2 2 0 01-3.4 0" />
  </svg>
)

export const IconGear = ({ size = 18 }) => (
  <svg {...S({ size })}>
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 11-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 11-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 11-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 110-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 112.83-2.83l.06.06a1.65 1.65 0 001.82.33h.01a1.65 1.65 0 001-1.51V3a2 2 0 114 0v.09a1.65 1.65 0 001 1.51h.01a1.65 1.65 0 001.82-.33l.06-.06a2 2 0 112.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82v.01a1.65 1.65 0 001.51 1H21a2 2 0 110 4h-.09a1.65 1.65 0 00-1.51 1z" />
  </svg>
)

export const IconSearch = ({ size = 16 }) => (
  <svg {...S({ size })}>
    <circle cx="11" cy="11" r="7" />
    <path d="M20 20l-3.5-3.5" />
  </svg>
)

export const IconPlus = ({ size = 16 }) => (
  <svg {...S({ size })}>
    <path d="M12 5v14M5 12h14" />
  </svg>
)

export const IconRefresh = ({ size = 16 }) => (
  <svg {...S({ size })}>
    <path d="M21 12a9 9 0 11-2.64-6.36M21 3v6h-6" />
  </svg>
)

export const IconBack = ({ size = 16 }) => (
  <svg {...S({ size })}>
    <path d="M15 18l-6-6 6-6" />
  </svg>
)

export const IconCopy = ({ size = 14 }) => (
  <svg {...S({ size })}>
    <rect x="9" y="9" width="12" height="12" rx="2" />
    <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
  </svg>
)

export const IconTrash = ({ size = 15 }) => (
  <svg {...S({ size })}>
    <path d="M3 6h18M8 6V4a1 1 0 011-1h6a1 1 0 011 1v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6" />
  </svg>
)

export const IconPencil = ({ size = 14 }) => (
  <svg {...S({ size })}>
    <path d="M17 3a2.83 2.83 0 114 4L7.5 20.5 2 22l1.5-5.5L17 3z" />
  </svg>
)

export const IconX = ({ size = 16 }) => (
  <svg {...S({ size })}>
    <path d="M18 6L6 18M6 6l12 12" />
  </svg>
)

export const IconCheck = ({ size = 16 }) => (
  <svg {...S({ size })}>
    <path d="M20 6L9 17l-5-5" />
  </svg>
)

export const IconMinus = ({ size = 14 }) => (
  <svg {...S({ size })}>
    <path d="M5 12h14" />
  </svg>
)

export const IconPower = ({ size = 14 }) => (
  <svg {...S({ size })}>
    <path d="M12 3v9" />
    <path d="M18.4 6.6a9 9 0 11-12.8 0" />
  </svg>
)

export const IconSquare = ({ size = 12 }) => (
  <svg {...S({ size, stroke: 1.9 })}>
    <rect x="5" y="5" width="14" height="14" rx="2.5" />
  </svg>
)

export const IconAlert = ({ size = 15 }) => (
  <svg {...S({ size })}>
    <path d="M10.3 3.9L1.8 18a2 2 0 001.7 3h17a2 2 0 001.7-3L13.7 3.9a2 2 0 00-3.4 0z" />
    <path d="M12 9v4M12 17h.01" />
  </svg>
)

export const IconPin = ({ size = 13 }) => (
  <svg {...S({ size })}>
    <path d="M20 10c0 6-8 12-8 12s-8-6-8-12a8 8 0 1116 0z" />
    <circle cx="12" cy="10" r="3" />
  </svg>
)

export const IconClock = ({ size = 13 }) => (
  <svg {...S({ size })}>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 7v5l3 3" />
  </svg>
)

export const IconExternal = ({ size = 13 }) => (
  <svg {...S({ size })}>
    <path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6M15 3h6v6M10 14L21 3" />
  </svg>
)

export const IconFolder = ({ size = 15 }) => (
  <svg {...S({ size })}>
    <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z" />
  </svg>
)

export const IconSparkle = ({ size = 14 }) => (
  <svg {...S({ size })}>
    <path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8L12 3z" />
    <path d="M19 15l.9 2.6L22.5 18.5l-2.6.9L19 22l-.9-2.6L15.5 18.5l2.6-.9L19 15z" />
  </svg>
)

export const IconTruck = ({ size = 16 }) => (
  <svg {...S({ size })}>
    <path d="M1 3h13v13H1zM14 8h4l3 3v5h-7V8z" />
    <circle cx="5.5" cy="18.5" r="2" />
    <circle cx="17.5" cy="18.5" r="2" />
  </svg>
)

export const IconGlobe = ({ size = 14 }) => (
  <svg {...S({ size })}>
    <circle cx="12" cy="12" r="9" />
    <path d="M3 12h18M12 3a14 14 0 010 18M12 3a14 14 0 000 18" />
  </svg>
)

export const IconShield = ({ size = 16 }) => (
  <svg {...S({ size })}>
    <path d="M12 3l7 3v5c0 5-3.5 8-7 9-3.5-1-7-4-7-9V6z" />
    <path d="M9.5 12l2 2 3.5-4" />
  </svg>
)

export const IconUser = ({ size = 16 }) => (
  <svg {...S({ size })}>
    <circle cx="12" cy="8" r="4" />
    <path d="M4 21c0-4 3.6-6.5 8-6.5s8 2.5 8 6.5" />
  </svg>
)
