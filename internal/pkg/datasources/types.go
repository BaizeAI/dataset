package datasources

type Type string

const (
	TypeS3          Type = "S3"
	TypeGit         Type = "GIT"
	TypeHTTP        Type = "HTTP"
	TypeConda       Type = "CONDA"
	TypeHuggingFace Type = "HUGGING_FACE"
	TypeModelScope  Type = "MODEL_SCOPE"
	TypeDatabase    Type = "DATABASE"
	TypeHadoop      Type = "HADOOP"
)

var (
	SupportedTypesString = []string{string(TypeS3), string(TypeGit), string(TypeHTTP), string(TypeConda), string(TypeHuggingFace), string(TypeModelScope)}
	SupportedTypes       = []Type{TypeS3, TypeGit, TypeHTTP, TypeConda, TypeHuggingFace, TypeModelScope}
)
