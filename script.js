import http from 'k6/http';

export default function () {
    const payload = JSON.stringify({
        long_url: "https://example.com",
        custom_alias: `alias-${__VU}-${__ITER}`,
        expire_in_days: 1,
    });

    http.post("http://localhost:8080/shorten", payload, {
        headers: { 'Content-Type': 'application/json' },
    });
}
