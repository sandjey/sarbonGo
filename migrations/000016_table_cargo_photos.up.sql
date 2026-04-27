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
-- Name: cargo_photos; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_photos (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    uploader_id uuid,
    mime character varying(128) NOT NULL,
    size_bytes bigint NOT NULL,
    path character varying(1024) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: cargo_photos cargo_photos_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_photos
    ADD CONSTRAINT cargo_photos_pkey PRIMARY KEY (id);


--
-- Name: idx_cargo_photos_cargo_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_photos_cargo_created ON public.cargo_photos USING btree (cargo_id, created_at DESC);


--
-- Name: cargo_photos cargo_photos_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_photos
    ADD CONSTRAINT cargo_photos_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


