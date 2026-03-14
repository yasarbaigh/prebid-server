# CD Tests (Custom Prebid OpenRTB 2.5 Tests)

This package contains custom unit and integration tests written specifically for the `openrtb_2_5/auction_handler.go` custom real-time bidding server logic.

## Overview
The real-time bidding auction handler is responsible for ingesting bid requests from Supply Side Platforms (SSPs), fan-outing HTTP calls to integrated Demand Side Platforms (DSPs), running a 1st price auction, applying appropriate revenue margins, injecting tracking data via AES-GCM encrypted urls, and returning a final JSON bid payload safely within the TMax deadline.

## Tests Written

### Unit Tests (`auction_handler_test.go`)
- `TestAuctionHandler_NoContentWhenAdServingDisabled`: Asserts that when `AdServing` is explicitly disabled in the system state, the server aborts the auction preemptively by replying with a `204 No Content` to save server bandwidth and CPU.
- `TestAuctionHandler_MissingIdentificationCode`: Validates the handler's requirement for the request to have a mandatory URL `c` query parameter acting as the SSP identifier account code.
- `TestAuctionHandler_InvalidJSON`: Ensures that the server gracefully rejects misconfigured JSON payloads via an HTTP `400 Bad Request`.

### Integration Tests (`integration_test.go`)
- `TestIntegration_AuctionHandlerSuccess`: An end-to-end simulation test testing the success path of an auction. This creates a mock HTTP server representing the DSP. It then crafts a fake `openrtb2.BidRequest` coming from a simulated SSP, passes it through the full handler pipeline.
It checks if:
1. The SSP identity is successfully translated to tenant configuration.
2. The bidding request gets forwarded to our mock DSP.
3. The mock DSP successfully evaluates the bid floor and constructs its response.
4. The handler correctly parses the final bid and returns an HTTP `200 OK` code along with the serialized seat-bid payload to the SSP.

## How to run
You can execute the entire custom test suite rapidly from the prebid root directly.
```bash
cd /opt/adserving/14-feb-2026/1_prebid-server
go test ./cd_tests/...
```
