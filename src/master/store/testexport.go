package store

const MetaTransportJSON = metaTransportJSON

func (s *Store) MetaSet(key, value string) error {
	return s.metaSet(key, value)
}
