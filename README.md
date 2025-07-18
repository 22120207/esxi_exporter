# esxi_exporter
esxi_exporter

```
env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags netgo -o esxi_exporter ./

env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -ldflags="-s -w" -tags netgo,osusergo,netgo -o esxi_exporter ./
```

```
export VCENTER_IP=10.0.100.251
export CREDS=$(echo -n 'quanly:Q35Ppyg0mJiQFsMS3fKu' | base64)

export TOKEN=$(curl -sk -X POST \
  -H "Authorization: Basic ${CREDS}" \
  https://${VCENTER_IP}/api/session/ | tr -d '"')

echo "Session Token: $TOKEN"

curl -k -u root:Rbx9rKa8rS3evDJ https://localhost/rest/com/vmware/cis/session
```
wget --no-check-certificate \
     --http-user=root \
     --http-password='Rbx9rKa8rS3evDJ' \
     https://localhost/rest/com/vmware/cis/session \
     -O -