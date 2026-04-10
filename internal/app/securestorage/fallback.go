package securestorage

type fallbackStore struct {
	primary   Store
	secondary Store
}

func NewFallbackStore(primary, secondary Store) Store {
	return &fallbackStore{
		primary:   primary,
		secondary: secondary,
	}
}

func (s *fallbackStore) Name() string {
	return s.primary.Name() + "-with-" + s.secondary.Name() + "-fallback"
}

func (s *fallbackStore) Read() (Data, error) {
	data, err := s.primary.Read()
	if err == nil && data != nil {
		return data, nil
	}

	data, err = s.secondary.Read()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return Data{}, nil
	}
	return data, nil
}

func (s *fallbackStore) Write(data Data) (WriteResult, error) {
	primaryBefore, err := s.primary.Read()
	if err != nil {
		primaryBefore = nil
	}

	result, err := s.primary.Write(data)
	if err == nil {
		if primaryBefore == nil {
			_ = s.secondary.Delete()
		}
		return result, nil
	}

	fallbackResult, fallbackErr := s.secondary.Write(data)
	if fallbackErr != nil {
		return WriteResult{}, fallbackErr
	}

	if primaryBefore != nil {
		_ = s.primary.Delete()
	}
	return fallbackResult, nil
}

func (s *fallbackStore) Delete() error {
	primaryErr := s.primary.Delete()
	secondaryErr := s.secondary.Delete()
	if primaryErr != nil {
		return primaryErr
	}
	return secondaryErr
}
