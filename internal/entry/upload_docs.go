package entry

// The tus resumable upload endpoints are served by the tusd handler mounted in
// registerUploadRoutes, so they have no Gin handler of their own. The two
// functions below are documentation-only stubs: swag reads their annotations to
// include the endpoints in the generated OpenAPI specification. They are never
// called at runtime; the reference below keeps them from being reported as
// unused.
var _ = []any{
	uploadCreateDoc,
	uploadAppendDoc,
}

// uploadCreateDoc documents the tus upload creation endpoint.
//
//	@Summary		Create a tus resumable upload
//	@Description	Creates a new tus upload session for an update operation.
//	@Description	The operation must already exist and be in the PENDING state.
//	@Description	The Upload-Metadata header must contain the base64-encoded operation_id and filename values.
//	@Tags			Uploads
//	@Accept			application/offset+octet-stream
//	@Produce		json
//	@Param			Tus-Resumable	header		string			true	"Tus protocol version"	default(1.0.0)
//	@Param			Upload-Length	header		int				true	"Total upload size in bytes"
//	@Param			Upload-Metadata	header		string			true	"Base64-encoded metadata: operation_id and filename"
//
// @Success        201             {string}      string            "Upload created"
// @Header        201             {string}       Location          "URL of the created upload resource"
// @Failure        400             {string}      string            "missing operation_id metadata"
// @Failure        404             {string}      string            "operation not found"
// @Failure        409             {string}      string            "operation is not pending"
// @Failure        500             {string}      string            "repository error"
// @Router          /uploads [post]
func uploadCreateDoc() {}

// uploadAppendDoc documents the tus upload append endpoint.
//
//	@Summary		Append a chunk to a tus upload
//	@Description	Appends a chunk of bytes at Upload-Offset.
//	@Description	When the upload is complete the server computes the file SHA256, stores the file metadata and dispatches the operation to the target worker node.
//	@Tags			Uploads
//	@Accept			application/offset+octet-stream
//	@Produce		json
//	@Param			id				path		string				true	"Upload ID returned in the Location header of POST /uploads"
//	@Param			Tus-Resumable	header		string				true	"Tus protocol version"	default(1.0.0)
//	@Param			Upload-Offset	header		int					true	"Byte offset of this chunk within the upload"
//	@Param			body			body		binary				true	"Raw chunk bytes"
//	@Success		204				{string}	string				"Chunk accepted"
//
// @Header		204	{string}	Upload-Offset	"Final offset of the completed upload"
// @Failure		400				{object}	ErrorResponse		"invalid content type, offset or checksum"
// @Failure		404				{object}	ErrorResponse		"upload not found"
// @Router			/uploads/{id} [patch]
func uploadAppendDoc() {}
