package api

import "errors"


var (
    // Error returned when the request is not authorized (HTTP 401)
    ErrorUnauthorized = errors.New("unauthorized")

    // Error returned when access is forbidden (HTTP 403)
    ErrorPermissionDenied = errors.New("permission denied")

    // Error returned when the directory is not found (HTTP 404)
    ErrorDirNotFound = errors.New("directory not found")
)
