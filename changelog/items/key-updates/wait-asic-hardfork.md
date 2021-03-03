- Antfarm option `WaitForSync` was renamed to `WaitForAsicHardforkAndSync` and
  code was updated to wait for ASIC hardfork height and then for ants to be in
  sync. This prevents 2 known issues happening around hardfork height and
  improves Antfarm stability.