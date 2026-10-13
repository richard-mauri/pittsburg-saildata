## v366 / 1.15.0 mobile map footer spacing fix

- Advances the development build from **v365** to **v366** while retaining public/application version **1.15.0**.
- Fixes excessive empty space beneath the Location map on narrow screens (viewport width **760px or less**). The map footer switches to a column layout at this breakpoint, but its playback-transport wrapper had retained a `flex-basis` of `760px` intended for the desktop row layout. In the mobile column layout, that basis became a vertical size and could leave a large blank area even with playback controls hidden.
- Overrides `.map-footer-transports` within the existing mobile media query with `flex: 0 0 auto; width: 100%; max-width: none;`, allowing its height to follow the visible playback controls.
- The **Scale** status is left in its existing position after the footer transports and should now appear close below the map when those controls are hidden. Swell and Air Temperature playback controls remain unchanged when enabled.
- Desktop map footer behavior and all other application functionality are unchanged by the source patch.
- Validation: confirmed the v366 source contains the mobile CSS override and retained application version **1.15.0**. Mobile-browser behavior still needs device verification.
- `main.go` candidate SHA-256: `e0a7ff634f9a31d150286d2fd09c71a85684c9fe633bcb0193c8eb07c5fdc87e`.
