## 0.1.8

- Fix deadlock when retrieving version

## 0.1.7

- Expose SD-WAN Manager version in client
- Switch to `slog` package for logging

## 0.1.6

- Add DeleteBody() function

## 0.1.5

- Handle 429 responses including Retry-After header

## 0.1.4

- Retry on specific authentication failures

## 0.1.3

- Enhance authentication error messages

## 0.1.2

- Return an error if authentication fails
- Add mutex to synchronize authentications

## 0.1.1

- Only retry on 408 and 5xx HTTP response status codes
- Return response payload in case of HTTP errors

## 0.1.0

- Initial release
