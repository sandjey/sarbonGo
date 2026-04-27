--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: media_files; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.media_files (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    content_hash character varying(64) NOT NULL,
    kind character varying(20) NOT NULL,
    mime character varying(128) NOT NULL,
    size_bytes bigint NOT NULL,
    path character varying(1024) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: media_files media_files_content_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_files
    ADD CONSTRAINT media_files_content_hash_key UNIQUE (content_hash);


--
-- Name: media_files media_files_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.media_files
    ADD CONSTRAINT media_files_pkey PRIMARY KEY (id);


--
-- Name: idx_media_files_kind_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_media_files_kind_created ON public.media_files USING btree (kind, created_at DESC);


--
-- PostgreSQL database dump complete
--


