curl -k -s -X POST \
  -F 'version=1.3' \
  -F 'auth_user=o.midiyanto' \
  -F 'auth_pwd=Omi!2001010021' \
  --form-string 'json_data={
    "operation": "core/get",
    "class": "Holiday",
    "key": "SELECT Holiday"",
    "output_fields": "date"
  }' \
  https://servicedesk.satnusa.com/webservices/rest.php | jq