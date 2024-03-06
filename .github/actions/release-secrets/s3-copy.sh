set -e

# Use sed to remove potential whitespace around the '=' in the pairs.
# Then split the string based on ',' to get an array of pairs.
IFS=',' read -ra pairs <<< $(echo "${PATH_PAIRS}" | sed 's/[[:space:]]*=[[:space:]]*/=/g')

for pair in "${pairs[@]}"; do
  # Split each pair using '=' as the delimiter
  IFS='=' read -r path value <<< "${pair}"

  aws s3 cp s3://"${path}" "${value}"
done
